// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type userSecretPlanTestBackend struct {
	consoleapi.Backend
	target   string
	relation consoleapi.IAMRelationships
	binding  consoleapi.IAMBindings
	write    func(consoleapi.IAMRelationships, string) (consoleapi.IAMRelationshipMutationResult, error)
	writes   atomic.Int32
}

func (b *userSecretPlanTestBackend) IAMStorageTarget() string { return b.target }
func (b *userSecretPlanTestBackend) IAMRelationships(context.Context, string, string) (consoleapi.IAMRelationships, error) {
	return b.relation, nil
}
func (b *userSecretPlanTestBackend) IAMBindings(context.Context, string, string) (consoleapi.IAMBindings, error) {
	return b.binding, nil
}
func (b *userSecretPlanTestBackend) IAMRotateRelationshipUserSecret(_ context.Context, _ consoleapi.IAMBindings, relation consoleapi.IAMRelationships, secret string) (consoleapi.IAMRelationshipMutationResult, error) {
	b.writes.Add(1)
	return b.write(relation, secret)
}
func secretPlanBackend(sess *session, target string) *userSecretPlanTestBackend {
	b := &userSecretPlanTestBackend{Backend: sess.runtime.backend, target: "fixed-storage"}
	b.relation = consoleapi.IAMRelationships{Kind: "user", Target: target, State: "known", StorageTarget: b.target, CoupledUserSecret: true, Revision: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(30 * time.Second), AffectedAccessKeys: []string{target, "service-child"}}
	b.binding = consoleapi.IAMBindings{Kind: "user", Target: target, Conditional: true, Revision: strings.Repeat("b", 64), Policies: []string{}}
	b.write = func(r consoleapi.IAMRelationships, _ string) (consoleapi.IAMRelationshipMutationResult, error) {
		return consoleapi.IAMRelationshipMutationResult{Outcome: "confirmed", Impact: r}, nil
	}
	sess.runtime.backend = b
	return b
}
func findNativePlanSession(s *Server, user string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.nativeUser == user {
			return sess
		}
	}
	return nil
}
func TestNativeUserSecretPlanServerOwnershipExpiryAndSingleUse(t *testing.T) {
	s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
	nativeSignIn(t, s, "alice")
	nativeSignIn(t, s, "bob")
	actor := findNativePlanSession(s, "alice")
	other := findNativePlanSession(s, "bob")
	backend := secretPlanBackend(actor, "alice")
	plan, err := prepareNativeUserSecretPlan(context.Background(), actor, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if plan.consume(other, time.Now()) {
		t.Fatal("plan transferred between sessions")
	}
	backend.relation.AffectedAccessKeys[0] = "bob"
	if plan.relation.AffectedAccessKeys[0] != "alice" || plan.impact.scopes[0] != nativeUserScope("alice") {
		t.Fatal("mutable query response changed captured authority")
	}
	if plan.consume(actor, plan.relation.ExpiresAt) {
		t.Fatal("expired plan accepted")
	}
	if !plan.consume(actor, time.Now()) || plan.consume(actor, time.Now()) {
		t.Fatal("plan reuse accepted")
	}
	backend.relation.AffectedAccessKeys[0] = "alice"
	backend.relation.StorageTarget = "foreign"
	if _, err := prepareNativeUserSecretPlan(context.Background(), actor, "alice"); err == nil {
		t.Fatal("foreign target inferred as expected target")
	}
	actor.nativeUser = ""
	backend.relation.StorageTarget = backend.target
	if _, err := prepareNativeUserSecretPlan(context.Background(), actor, "alice"); err == nil {
		t.Fatal("OIDC subject inferred as native parent")
	}
}

type secretPlanResponseProbe struct {
	*httptest.ResponseRecorder
	onWrite func()
}

func (w secretPlanResponseProbe) WriteHeader(status int) {
	w.onWrite()
	w.ResponseRecorder.WriteHeader(status)
}
func TestNativeUserSecretPlanRetirementAndResponseOrdering(t *testing.T) {
	for _, scenario := range []string{"self-confirmed", "other-confirmed", "unknown", "denied", "conflict", "unexpected-impact"} {
		t.Run(scenario, func(t *testing.T) {
			s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
			alice, _ := nativeSignIn(t, s, "alice")
			bob, _ := nativeSignIn(t, s, "bob")
			child, _ := nativeSignIn(t, s, "service-child")
			actor := findNativePlanSession(s, "alice")
			target := "alice"
			if scenario == "other-confirmed" {
				target = "bob"
			}
			backend := secretPlanBackend(actor, target)
			backend.write = func(relation consoleapi.IAMRelationships, secret string) (consoleapi.IAMRelationshipMutationResult, error) {
				if secret != "new-user-secret" {
					t.Error("wrong secret dispatched")
				}
				switch scenario {
				case "unknown":
					return consoleapi.IAMRelationshipMutationResult{}, &consoleapi.Error{Status: 502, Code: "outcome_unknown"}
				case "denied":
					return consoleapi.IAMRelationshipMutationResult{}, &consoleapi.Error{Status: 403, Code: "AccessDenied"}
				case "conflict":
					return consoleapi.IAMRelationshipMutationResult{}, &consoleapi.Error{Status: 409, Code: "identity_revision_conflict"}
				case "unexpected-impact":
					relation.AffectedAccessKeys = []string{"bob"}
				}
				return consoleapi.IAMRelationshipMutationResult{Outcome: "confirmed", Impact: relation}, nil
			}
			plan, err := prepareNativeUserSecretPlan(context.Background(), actor, target)
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			observed := false
			probe := secretPlanResponseProbe{recorder, func() {
				observed = true
				s.mu.Lock()
				defer s.mu.Unlock()
				exists := false
				for _, sess := range s.sessions {
					if sess == actor {
						exists = true
					}
				}
				if !exists {
					t.Error("actor retired before writing own response")
				}
				if scenario == "other-confirmed" {
					for _, sess := range s.sessions {
						if sess.nativeUser == "bob" || sess.nativeUser == "service-child" {
							t.Error("other affected user survived acknowledgement")
						}
					}
				}
			}}
			s.executeNativeUserSecretPlan(probe, httptest.NewRequest(http.MethodPost, "/", nil), actor, plan, "new-user-secret")
			if !observed || backend.writes.Load() != 1 || strings.Contains(recorder.Body.String(), "new-user-secret") {
				t.Fatal("response or secret safety failed")
			}
			want := 200
			if scenario == "unknown" || scenario == "unexpected-impact" {
				want = 502
			}
			if scenario == "denied" {
				want = 403
			}
			if scenario == "conflict" {
				want = 409
			}
			if recorder.Code != want {
				t.Fatalf("response %d want %d", recorder.Code, want)
			}
			reject := scenario == "denied" || scenario == "conflict"
			wantAlice := 401
			if reject || scenario == "other-confirmed" {
				wantAlice = 200
			}
			if nativeSessionStatus(s, alice) != wantAlice {
				t.Fatal("actor retirement wrong")
			}
			wantBob := 200
			if scenario == "other-confirmed" {
				wantBob = 401
			}
			if nativeSessionStatus(s, bob) != wantBob {
				t.Fatal("unaffected user retired")
			}
			wantChild := 401
			if reject {
				wantChild = 200
			}
			if nativeSessionStatus(s, child) != wantChild {
				t.Fatal("child impact omitted")
			}
			replay := httptest.NewRecorder()
			s.executeNativeUserSecretPlan(replay, httptest.NewRequest(http.MethodPost, "/", nil), actor, plan, "new-user-secret")
			if replay.Code != 409 || backend.writes.Load() != 1 {
				t.Fatal("same plan dispatched twice")
			}
		})
	}
}
