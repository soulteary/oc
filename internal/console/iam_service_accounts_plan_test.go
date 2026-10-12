// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type servicePlanFixture struct {
	*fakeBackend
	target          string
	relation        consoleapi.IAMRelationships
	result          consoleapi.IAMServiceAccountMutationResult
	err             error
	queries, writes int
}

func (f *servicePlanFixture) IAMStorageTarget() string { return f.target }
func (f *servicePlanFixture) IAMRelationships(_ context.Context, kind, target string) (consoleapi.IAMRelationships, error) {
	f.queries++
	if kind != "service-account" || target != "service-key" {
		return consoleapi.IAMRelationships{}, errUnknownIdentityScope
	}
	return f.relation, nil
}
func (f *servicePlanFixture) IAMMutateRelationshipServiceAccount(context.Context, consoleapi.IAMActionRequest, consoleapi.IAMRelationships) (consoleapi.IAMServiceAccountMutationResult, error) {
	f.writes++
	return f.result, f.err
}
func newServicePlanFixture() (*servicePlanFixture, *session) {
	f := &servicePlanFixture{fakeBackend: &fakeBackend{}, target: "https://store\x00https://admin"}
	f.relation = consoleapi.IAMRelationships{Kind: "service-account", Target: "service-key", NativeParent: "native-parent", State: "known", CoupledServiceAccounts: true, Revision: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(20 * time.Second), AffectedAccessKeys: []string{"service-key"}, StorageTarget: f.target}
	f.result = consoleapi.IAMServiceAccountMutationResult{Outcome: "confirmed", ServiceAccount: consoleapi.IAMServiceAccount{AccessKey: "service-key", ParentUser: "native-parent", Status: "off"}, Impact: f.relation}
	sess := &session{nativeUser: "native-admin", ctx: context.Background(), runtime: sessionRuntime{backend: f, identity: strings.Repeat("b", 64), lifetime: &backendLifetime{}}}
	return f, sess
}
func servicePlanAction() consoleapi.IAMActionRequest {
	return consoleapi.IAMActionRequest{Action: "service-account.disable", AccessKey: "service-key", ConfirmTarget: "service-key"}
}
func TestServicePlanSingleSnapshotOwnerConnectionExpiryAndCopies(t *testing.T) {
	f, actor := newServicePlanFixture()
	plan, err := prepareNativeServiceAccountPlan(context.Background(), actor, servicePlanAction())
	if err != nil {
		t.Fatal(err)
	}
	if f.queries != 1 || len(plan.scopes) != 1 || plan.scopes[0].key != "service-key" {
		t.Fatal("parent or sibling became impact")
	}
	other := *actor
	if plan.consume(&other, time.Now()) {
		t.Fatal("plan accepted other session")
	}
	old := actor.runtime.backend
	actor.runtime.backend = &fakeBackend{}
	if plan.consume(actor, time.Now()) {
		t.Fatal("plan accepted replacement backend")
	}
	actor.runtime.backend = old
	if plan.consume(actor, plan.relation.ExpiresAt) {
		t.Fatal("expired proof consumed")
	}
	copied := *plan
	if !plan.consume(actor, time.Now()) || copied.consume(actor, time.Now()) {
		t.Fatal("copied plan replayed")
	}
}
func TestServicePlanRejectsOIDCUnknownParentAndExpandedScope(t *testing.T) {
	for _, scenario := range []string{"oidc", "no-parent", "self-parent", "wrong-target", "no-capability", "expanded", "expired"} {
		f, actor := newServicePlanFixture()
		switch scenario {
		case "oidc":
			actor.oidc = &oidcSession{}
		case "no-parent":
			f.relation.NativeParent = ""
		case "self-parent":
			f.relation.NativeParent = "service-key"
		case "wrong-target":
			f.relation.StorageTarget = "other"
		case "no-capability":
			f.relation.CoupledServiceAccounts = false
		case "expanded":
			f.relation.AffectedAccessKeys = []string{"service-key", "native-parent"}
		case "expired":
			f.relation.ExpiresAt = time.Now().Add(-time.Second)
		}
		if _, err := prepareNativeServiceAccountPlan(context.Background(), actor, servicePlanAction()); err == nil {
			t.Fatal(scenario, "accepted")
		}
		if scenario == "oidc" && f.queries != 0 {
			t.Fatal("OIDC borrowed native relationship authority")
		}
	}
}
func TestServicePlanKnownRejectionKeepsTargetUnknownRetiresOnlyTarget(t *testing.T) {
	for _, scenario := range []string{"confirmed", "conflict", "typed503", "changed-parent", "expanded"} {
		t.Run(scenario, func(t *testing.T) {
			s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
			target, _ := nativeSignIn(t, s, "service-key")
			parent, _ := nativeSignIn(t, s, "native-parent")
			sibling, _ := nativeSignIn(t, s, "sibling-key")
			admin, _ := nativeSignIn(t, s, "native-admin")
			var actor *session
			s.mu.Lock()
			for _, sess := range s.sessions {
				if sess.nativeUser == "native-admin" {
					actor = sess
				}
			}
			s.mu.Unlock()
			f, _ := newServicePlanFixture()
			actor.runtime.backend = f
			plan, err := prepareNativeServiceAccountPlan(context.Background(), actor, servicePlanAction())
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "conflict":
				f.err = &consoleapi.Error{Status: 409, Code: "identity_revision_conflict", Message: "stale"}
			case "typed503":
				f.err = &consoleapi.Error{Status: 503, Code: "partial_commit", Message: "unknown"}
			case "changed-parent":
				f.result.ServiceAccount.ParentUser = "other-parent"
			case "expanded":
				f.result.Impact.AffectedAccessKeys = []string{"service-key", "native-parent"}
			}
			w := httptest.NewRecorder()
			s.executeNativeServiceAccountPlan(w, httptest.NewRequest("POST", "/api/iam/actions", nil), actor, plan)
			wantTarget, wantHTTP := 401, 200
			if scenario == "conflict" {
				wantTarget, wantHTTP = 200, 409
			}
			if scenario == "typed503" || scenario == "changed-parent" || scenario == "expanded" {
				wantHTTP = 502
			}
			if w.Code != wantHTTP || nativeSessionStatus(s, target) != wantTarget || nativeSessionStatus(s, parent) != 200 || nativeSessionStatus(s, sibling) != 200 || nativeSessionStatus(s, admin) != 200 {
				t.Fatal("service account retirement escaped its scope", scenario, w.Code)
			}
			if f.writes != 1 || len(s.writeSlots) != 0 || len(s.nativeUserChanging) != 0 {
				t.Fatal("retry or leaked barrier")
			}
		})
	}
}
