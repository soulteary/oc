// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type nativeServiceCreationTestBackend struct {
	*userMutationPlanTestBackend
	onQuery func()
	failure error
}

func (b *nativeServiceCreationTestBackend) IAMRelationships(ctx context.Context, kind, target string) (consoleapi.IAMRelationships, error) {
	if b.onQuery != nil {
		b.onQuery()
	}
	return b.userMutationPlanTestBackend.IAMRelationships(ctx, kind, target)
}
func (b *nativeServiceCreationTestBackend) IAMCreateRelationshipServiceAccount(_ context.Context, args consoleapi.IAMActionRequest, relation consoleapi.IAMRelationships) (consoleapi.IAMActionResult, error) {
	b.writes.Add(1)
	return consoleapi.IAMActionResult{Outcome: "confirmed", Credentials: &consoleapi.IAMCredentials{AccessKey: "created-child", SecretKey: "test-generated-secret"}}, b.failure
}
func TestNativeServiceCreationRequiresFreshOwnedCapability(t *testing.T) {
	for _, scenario := range []string{"confirmed", "denied", "unknown", "missing-capability", "expired", "actor-changed", "namespace-changed", "missing-parent"} {
		t.Run(scenario, func(t *testing.T) {
			s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
			alice, _ := nativeSignIn(t, s, "alice")
			actor := findNativePlanSession(s, "alice")
			base := userMutationBackend(actor, "alice")
			base.relation.CoupledServiceCreation = true
			backend := &nativeServiceCreationTestBackend{userMutationPlanTestBackend: base}
			actor.runtime.backend = backend
			originalIdentity := actor.runtime.identity
			defer func() { actor.runtime.identity = originalIdentity }()
			switch scenario {
			case "denied":
				backend.failure = &consoleapi.Error{Status: 403, Code: "iam_action_denied"}
			case "unknown":
				backend.failure = &consoleapi.Error{Status: 502, Code: "outcome_unknown"}
			case "missing-capability":
				base.relation.CoupledServiceCreation = false
			case "expired":
				base.relation.ExpiresAt = time.Now().Add(-time.Second)
			case "actor-changed":
				backend.onQuery = func() { actor.runtime.identity = "changed" }
			case "namespace-changed":
				backend.onQuery = func() { base.target = "foreign" }
			case "missing-parent":
				base.relation.AffectedAccessKeys = []string{"service-child"}
			}
			w := httptest.NewRecorder()
			s.createNativeServiceAccount(w, httptest.NewRequest("POST", "/api/iam/actions", nil), actor, consoleapi.IAMActionRequest{Action: "service-account.create", User: "alice", ConfirmTarget: "alice"})
			wantDispatch := scenario == "confirmed" || scenario == "denied" || scenario == "unknown"
			if (base.writes.Load() == 1) != wantDispatch {
				t.Fatal("capability/ownership guard dispatched stale creation")
			}
			want := 200
			if scenario == "denied" {
				want = 403
			}
			if scenario == "unknown" {
				want = 502
			}
			if wantDispatch && w.Code != want {
				t.Fatalf("status %d want %d", w.Code, want)
			}
			if !wantDispatch && w.Code < 400 {
				t.Fatal("stale creation confirmed")
			}
			actor.runtime.identity = originalIdentity
			if nativeSessionStatus(s, alice) != 200 || len(s.writeSlots) != 0 {
				t.Fatal("creation invalidated existing identity or leaked write slot")
			}
		})
	}
}
