// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type groupRelationFixture struct {
	*fakeBackend
	actor     *session
	snapshots map[string]consoleapi.IAMRelationships
	queries   []string
	target    string
}

func (f *groupRelationFixture) IAMStorageTarget() string { return f.target }
func (f *groupRelationFixture) IAMRelationships(_ context.Context, kind, target string) (consoleapi.IAMRelationships, error) {
	f.queries = append(f.queries, kind+":"+target)
	return f.snapshots[kind+":"+target], nil
}
func groupFixture() *groupRelationFixture {
	target := "https://store\x00https://admin"
	snapshot := func(kind, name string, keys []string) consoleapi.IAMRelationships {
		return consoleapi.IAMRelationships{Kind: kind, Target: name, State: "known", Revision: strings.Repeat("a", 64), CoupledGroups: true, StorageTarget: target, ExpiresAt: time.Now().Add(20 * time.Second), AffectedAccessKeys: keys}
	}
	fixture := &groupRelationFixture{fakeBackend: &fakeBackend{}, target: target, snapshots: map[string]consoleapi.IAMRelationships{"group:team": snapshot("group", "team", []string{"old-user", "old-child"}), "user:new-user": snapshot("user", "new-user", []string{"new-user", "new-child"})}}
	fixture.actor = &session{nativeUser: "actor", ctx: context.Background(), runtime: sessionRuntime{backend: fixture}}
	return fixture
}
func TestGroupPlanCapturesOldNewAndChildrenWithoutAliasing(t *testing.T) {
	f := groupFixture()
	members := []string{"new-user"}
	plan, err := prepareIAMGroupMutation(context.Background(), f.actor, consoleapi.IAMActionRequest{Action: "group.add-members", Group: "team", ConfirmTarget: "team", Members: members})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.queries, []string{"group:team", "user:new-user"}) {
		t.Fatal("group fingerprint must precede every user query", f.queries)
	}
	if !reflect.DeepEqual(plan.relation.AffectedAccessKeys, []string{"new-child", "new-user", "old-child", "old-user"}) {
		t.Fatal("child impact missing", plan.relation.AffectedAccessKeys)
	}
	members[0] = "changed"
	f.snapshots["group:team"].AffectedAccessKeys[0] = "changed"
	if plan.args.Members[0] != "new-user" || plan.scopes[3].key != "old-user" {
		t.Fatal("plan aliased mutable drafts")
	}
}
func TestGroupPlanRejectsUnknownExpiredAndCrossTarget(t *testing.T) {
	for _, kind := range []string{"unknown", "expired", "wrong-target", "missing-capability", "missing-members", "nil-impact"} {
		f := groupFixture()
		snapshot := f.snapshots["user:new-user"]
		switch kind {
		case "unknown":
			snapshot.State = "unknown"
		case "expired":
			snapshot.ExpiresAt = time.Now().Add(-time.Second)
		case "wrong-target":
			snapshot.StorageTarget = "other"
		case "missing-members":
			snapshot.AffectedAccessKeys = []string{}
		case "nil-impact":
			snapshot.AffectedAccessKeys = nil
		case "missing-capability":
			group := f.snapshots["group:team"]
			group.CoupledGroups = false
			f.snapshots["group:team"] = group
		}
		f.snapshots["user:new-user"] = snapshot
		if _, err := prepareIAMGroupMutation(context.Background(), f.actor, consoleapi.IAMActionRequest{Action: "group.add-members", Group: "team", ConfirmTarget: "team", Members: []string{"new-user"}}); err == nil {
			t.Fatal(kind, "accepted")
		}
	}
}
func TestGroupPlanKnownEmptyAndResponseCannotChangeRetirement(t *testing.T) {
	f := groupFixture()
	snapshot := f.snapshots["group:team"]
	snapshot.AffectedAccessKeys = []string{}
	f.snapshots["group:team"] = snapshot
	plan, err := prepareIAMGroupMutation(context.Background(), f.actor, consoleapi.IAMActionRequest{Action: "group.delete", Group: "team", ConfirmTarget: "team"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.scopes == nil || !(identityImpact{known: true, scopes: plan.scopes}).valid() {
		t.Fatal("known empty lost")
	}
	result := consoleapi.IAMGroupMutationResult{Outcome: "confirmed", Group: consoleapi.IAMGroup{Name: "team", Status: "deleted", Members: []string{}, Policies: []string{}}, Impact: plan.relation}
	// The captured impact remains authoritative after response expiry; protocol
	// proof verification belongs to the transport before acknowledgement delivery.
	result.Impact.ExpiresAt = time.Now().Add(-time.Second)
	if !plan.responseMatches(result) {
		t.Fatal("expired acknowledgement changed retirement scope")
	}
	result.Impact.AffectedAccessKeys = []string{"unrelated"}
	if plan.responseMatches(result) {
		t.Fatal("response overwrote captured scope")
	}
}

type groupMutationFixture struct {
	result consoleapi.IAMGroupMutationResult
	err    error
	calls  int
}

func (f *groupMutationFixture) IAMMutateRelationshipGroup(context.Context, consoleapi.IAMActionRequest, consoleapi.IAMRelationships) (consoleapi.IAMGroupMutationResult, error) {
	f.calls++
	return f.result, f.err
}
func TestGroupExecutionRejectionKeepsSessionsUnknownRetiresCapturedSet(t *testing.T) {
	for _, scenario := range []string{"confirmed", "conflict", "unknown", "changed-impact", "expired-plan", "typed503"} {
		t.Run(scenario, func(t *testing.T) {
			s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
			alice, _ := nativeSignIn(t, s, "alice")
			bob, _ := nativeSignIn(t, s, "bob")
			var actor *session
			s.mu.Lock()
			for _, sess := range s.sessions {
				if sess.nativeUser == "bob" {
					actor = sess
				}
			}
			s.mu.Unlock()
			relation := consoleapi.IAMRelationships{Kind: "group", Target: "team", State: "known", CoupledGroups: true, StorageTarget: "store", AffectedAccessKeys: []string{"alice"}}
			plan := iamGroupMutationPlan{args: consoleapi.IAMActionRequest{Action: "group.disable", Group: "team", ConfirmTarget: "team"}, relation: relation, scopes: []identityScope{nativeUserScope("alice")}, expires: time.Now().Add(time.Minute)}
			backend := &groupMutationFixture{result: consoleapi.IAMGroupMutationResult{Outcome: "confirmed", Group: consoleapi.IAMGroup{Name: "team", Status: "disabled", Members: []string{}, Policies: []string{}}, Impact: relation}}
			switch scenario {
			case "expired-plan":
				plan.expires = time.Now().Add(-time.Second)
			case "conflict":
				backend.err = &consoleapi.Error{Status: 409, Code: "identity_revision_conflict", Message: "stale graph"}
			case "typed503":
				backend.err = &consoleapi.Error{Status: 503, Code: "propagation_partial", Message: "may have committed"}
			case "unknown":
				backend.err = &consoleapi.Error{Status: 502, Code: "outcome_unknown", Message: "lost acknowledgement"}
			case "changed-impact":
				backend.result.Impact.AffectedAccessKeys = []string{"bob"}
			}
			w := httptest.NewRecorder()
			plan.owned = actor.runtime.backend
			plan.owner = actor
			plan.backend = backend
			plan.use = &iamGroupPlanUse{}
			s.executeIAMGroupMutation(w, httptest.NewRequest("POST", "/api/iam/actions", nil), actor, plan)
			wantHTTP := 200
			if scenario == "conflict" || scenario == "expired-plan" {
				wantHTTP = 409
			}
			if scenario == "unknown" || scenario == "changed-impact" || scenario == "typed503" {
				wantHTTP = 502
			}
			if w.Code != wantHTTP {
				t.Fatal("wrong result classification", scenario, w.Code)
			}
			expected := 401
			if scenario == "conflict" || scenario == "expired-plan" {
				expected = 200
			}
			if nativeSessionStatus(s, alice) != expected || nativeSessionStatus(s, bob) != 200 {
				t.Fatal("retirement escaped captured group scope", scenario)
			}
			if len(s.writeSlots) != 0 || len(s.nativeUserChanging) != 0 || (scenario != "expired-plan" && backend.calls != 1) || (scenario == "expired-plan" && backend.calls != 0) {
				t.Fatal("barrier leak or mutation retry")
			}
		})
	}
}

func (f *groupRelationFixture) IAMMutateRelationshipGroup(context.Context, consoleapi.IAMActionRequest, consoleapi.IAMRelationships) (consoleapi.IAMGroupMutationResult, error) {
	return consoleapi.IAMGroupMutationResult{}, nil
}

func TestGroupPlanRejectsCrossOwnerBackendReplacementAndCopiedReplay(t *testing.T) {
	f := groupFixture()
	plan, err := prepareIAMGroupMutation(context.Background(), f.actor, consoleapi.IAMActionRequest{Action: "group.disable", Group: "team", ConfirmTarget: "team"})
	if err != nil {
		t.Fatal(err)
	}
	// Use a real executor server with another session to prove a prepared plan
	// cannot migrate to a different actor or connection, even via a copied value.
	s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
	alice, _ := nativeSignIn(t, s, "alice")
	bob, _ := nativeSignIn(t, s, "bob")
	var actor, other *session
	s.mu.Lock()
	for _, sess := range s.sessions {
		if sess.nativeUser == "alice" {
			actor = sess
		}
		if sess.nativeUser == "bob" {
			other = sess
		}
	}
	s.mu.Unlock()
	backend := &groupMutationFixture{result: consoleapi.IAMGroupMutationResult{Outcome: "confirmed", Group: consoleapi.IAMGroup{Name: "team", Status: "disabled", Members: []string{}, Policies: []string{}}, Impact: plan.relation}}
	plan.owner = actor
	plan.owned = actor.runtime.backend
	plan.backend = backend
	plan.scopes = []identityScope{nativeUserScope("unrelated")}
	plan.relation.AffectedAccessKeys = []string{"unrelated"}
	backend.result.Impact = plan.relation
	invoke := func(sess *session, p iamGroupMutationPlan) int {
		w := httptest.NewRecorder()
		s.executeIAMGroupMutation(w, httptest.NewRequest("POST", "/api/iam/actions", nil), sess, p)
		return w.Code
	}
	if invoke(other, plan) != 409 || backend.calls != 0 {
		t.Fatal("another owner dispatched prepared mutation")
	}
	original := actor.runtime.backend
	actor.runtime.backend = &fakeBackend{}
	if invoke(actor, plan) != 409 || backend.calls != 0 {
		t.Fatal("replacement connection accepted old plan")
	}
	actor.runtime.backend = original
	copied := plan
	if invoke(actor, plan) != 200 || backend.calls != 1 {
		t.Fatal("own prepared change failed")
	}
	if invoke(actor, copied) != 409 || backend.calls != 1 {
		t.Fatal("copied plan replayed mutation")
	}
	if nativeSessionStatus(s, alice) != 200 || nativeSessionStatus(s, bob) != 200 {
		t.Fatal("unrelated captured impact retired users")
	}
}

func TestGroupPrepareRejectsOIDCWithoutRelationshipQueries(t *testing.T) {
	f := groupFixture()
	f.actor.oidc = &oidcSession{}
	if _, err := prepareIAMGroupMutation(context.Background(), f.actor, consoleapi.IAMActionRequest{Action: "group.disable", Group: "team", ConfirmTarget: "team"}); err == nil || len(f.queries) != 0 {
		t.Fatal("OIDC actor acquired native group plan")
	}
}
