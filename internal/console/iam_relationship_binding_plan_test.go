// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type bindingPlanTestBackend struct {
	*userSecretPlanTestBackend
	err    error
	result consoleapi.IAMRelationshipMutationResult
}

func (b *bindingPlanTestBackend) IAMReplaceRelationshipBindings(_ context.Context, _ consoleapi.IAMBindings, _ consoleapi.IAMRelationships, _ []string) (consoleapi.IAMRelationshipMutationResult, error) {
	b.writes.Add(1)
	return b.result, b.err
}
func TestRelationshipBindingPlanReviewedRevisionAndOwnership(t *testing.T) {
	s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
	nativeSignIn(t, s, "alice")
	nativeSignIn(t, s, "bob")
	owner := findNativePlanSession(s, "alice")
	other := findNativePlanSession(s, "bob")
	b := &bindingPlanTestBackend{userSecretPlanTestBackend: secretPlanBackend(owner, "alice")}
	b.relation.CoupledBindings = true
	owner.runtime.backend = b
	args := consoleapi.IAMActionRequest{Action: "user.policies", User: "alice", ConfirmTarget: "alice", Revision: b.binding.Revision, Policies: []string{"read-policy"}}
	bad := args
	bad.Revision = b.relation.Revision
	if _, err := prepareNativeRelationshipBindingPlan(context.Background(), owner, bad); err == nil {
		t.Fatal("stale review accepted")
	}
	plan, err := prepareNativeRelationshipBindingPlan(context.Background(), owner, args)
	if err != nil {
		t.Fatal(err)
	}
	args.Policies[0] = "mutated"
	b.relation.AffectedAccessKeys[0] = "bob"
	if plan.policies[0] != "read-policy" || plan.relation.AffectedAccessKeys[0] != "alice" {
		t.Fatal("plan aliases browser/backend mutable data")
	}
	if plan.consume(other) || !plan.consume(owner) || plan.consume(owner) {
		t.Fatal("plan transferred or replayed")
	}
	if b.writes.Load() != 0 {
		t.Fatal("preflight dispatched")
	}
}
func TestRelationshipBindingPlanRetiresCapturedScopeOnUnknown(t *testing.T) {
	for _, scenario := range []string{"confirmed", "unknown", "denied", "foreign-impact"} {
		t.Run(scenario, func(t *testing.T) {
			s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
			alice, _ := nativeSignIn(t, s, "alice")
			bob, _ := nativeSignIn(t, s, "bob")
			owner := findNativePlanSession(s, "alice")
			b := &bindingPlanTestBackend{userSecretPlanTestBackend: secretPlanBackend(owner, "alice")}
			b.relation.CoupledBindings = true
			owner.runtime.backend = b
			b.result = consoleapi.IAMRelationshipMutationResult{Outcome: "confirmed", Impact: b.relation, Bindings: b.binding}
			if scenario == "unknown" {
				b.err = errors.New("lost acknowledgement")
			}
			if scenario == "denied" {
				b.err = &consoleapi.Error{Status: 403, Code: "access_denied", Message: "denied"}
			}
			if scenario == "foreign-impact" {
				b.result.Impact.StorageTarget = "foreign"
			}
			args := consoleapi.IAMActionRequest{Action: "user.policies", User: "alice", ConfirmTarget: "alice", Revision: b.binding.Revision, Policies: []string{}}
			plan, err := prepareNativeRelationshipBindingPlan(context.Background(), owner, args)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			s.executeNativeRelationshipBindingPlan(w, httptest.NewRequest("POST", "/api/iam/actions", nil), owner, plan)
			want := 401
			if scenario == "denied" {
				want = 200
			}
			if nativeSessionStatus(s, alice) != want || nativeSessionStatus(s, bob) != 200 || b.writes.Load() != 1 || len(s.writeSlots) != 0 {
				t.Fatal("retirement was not captured, scoped and single dispatch")
			}
		})
	}
}

func TestRelationshipPlansRejectOwnedBackendReplacement(t *testing.T) {
	s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
	nativeSignIn(t, s, "alice")
	owner := findNativePlanSession(s, "alice")
	b := &bindingPlanTestBackend{userSecretPlanTestBackend: secretPlanBackend(owner, "alice")}
	b.relation.CoupledBindings = true
	owner.runtime.backend = b
	args := consoleapi.IAMActionRequest{Action: "user.policies", User: "alice", ConfirmTarget: "alice", Revision: b.binding.Revision, Policies: []string{}}
	binding, err := prepareNativeRelationshipBindingPlan(context.Background(), owner, args)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := prepareNativeUserSecretPlan(context.Background(), owner, "alice")
	if err != nil {
		t.Fatal(err)
	}
	original := owner.runtime.backend
	owner.runtime.backend = &userSecretPlanTestBackend{Backend: original, target: b.target, relation: b.relation, binding: b.binding}
	if binding.consume(owner) || secret.consume(owner, time.Now()) {
		t.Fatal("plan followed a replacement client with the same storage target")
	}
	owner.runtime.backend = original
	if !binding.consume(owner) || !secret.consume(owner, time.Now()) {
		t.Fatal("rejected replacement consumed the original plan")
	}
}
