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

type userMutationPlanTestBackend struct {
	consoleapi.Backend
	target   string
	relation consoleapi.IAMRelationships
	write    func(consoleapi.IAMRelationships, consoleapi.IAMActionRequest) (consoleapi.IAMUserMutationResult, error)
	writes   atomic.Int32
}

func (b *userMutationPlanTestBackend) IAMStorageTarget() string { return b.target }
func (b *userMutationPlanTestBackend) IAMRelationships(context.Context, string, string) (consoleapi.IAMRelationships, error) {
	return b.relation, nil
}
func (b *userMutationPlanTestBackend) IAMMutateRelationshipUser(_ context.Context, args consoleapi.IAMActionRequest, relation consoleapi.IAMRelationships) (consoleapi.IAMUserMutationResult, error) {
	b.writes.Add(1)
	return b.write(relation, args)
}
func userMutationBackend(sess *session, target string) *userMutationPlanTestBackend {
	b := &userMutationPlanTestBackend{Backend: sess.runtime.backend, target: "fixed-storage"}
	b.relation = consoleapi.IAMRelationships{Kind: "user", Target: target, State: "known", StorageTarget: b.target, CoupledUsers: true, Revision: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(30 * time.Second), AffectedAccessKeys: []string{target, "service-child"}}
	b.write = func(r consoleapi.IAMRelationships, args consoleapi.IAMActionRequest) (consoleapi.IAMUserMutationResult, error) {
		return consoleapi.IAMUserMutationResult{Outcome: "confirmed", User: consoleapi.IAMUserMutationState{AccessKey: args.User, Status: "disabled"}, Impact: r}, nil
	}
	sess.runtime.backend = b
	return b
}
func TestNativeUserMutationPlanRetirementAndResponseOrdering(t *testing.T) {
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
			backend := userMutationBackend(actor, target)
			backend.write = func(relation consoleapi.IAMRelationships, args consoleapi.IAMActionRequest) (consoleapi.IAMUserMutationResult, error) {
				if args.Action != "user.disable" || args.User != target {
					t.Error("wrong action dispatched")
				}
				switch scenario {
				case "unknown":
					return consoleapi.IAMUserMutationResult{}, &consoleapi.Error{Status: 502, Code: "outcome_unknown"}
				case "denied":
					return consoleapi.IAMUserMutationResult{}, &consoleapi.Error{Status: 403, Code: "AccessDenied"}
				case "conflict":
					return consoleapi.IAMUserMutationResult{}, &consoleapi.Error{Status: 409, Code: "identity_revision_conflict"}
				case "unexpected-impact":
					relation.AffectedAccessKeys = []string{"bob"}
				}
				return consoleapi.IAMUserMutationResult{Outcome: "confirmed", User: consoleapi.IAMUserMutationState{AccessKey: target, Status: "disabled"}, Impact: relation}, nil
			}
			plan, err := prepareIAMUserMutation(context.Background(), actor, consoleapi.IAMActionRequest{Action: "user.disable", User: target, ConfirmTarget: target})
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
			s.executeIAMUserMutation(probe, httptest.NewRequest(http.MethodPost, "/", nil), actor, plan)
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
			s.executeIAMUserMutation(replay, httptest.NewRequest(http.MethodPost, "/", nil), actor, plan)
			if replay.Code != 409 || backend.writes.Load() != 1 {
				t.Fatal("same plan dispatched twice")
			}
		})
	}
}

func TestUserMutationPlanCannotTransferActorOrLifetime(t *testing.T) {
	for _, scenario := range []string{"actor", "identity", "lifetime", "namespace", "backend", "expired", "capability", "missing-parent"} {
		t.Run(scenario, func(t *testing.T) {
			s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
			nativeSignIn(t, s, "alice")
			actor := findNativePlanSession(s, "alice")
			backend := userMutationBackend(actor, "alice")
			originalRuntime := actor.runtime
			originalUser := actor.nativeUser
			defer func() { actor.runtime = originalRuntime; actor.nativeUser = originalUser }()
			args := consoleapi.IAMActionRequest{Action: "user.delete", User: "alice", ConfirmTarget: "alice"}
			if scenario == "capability" {
				backend.relation.CoupledUsers = false
			}
			if scenario == "missing-parent" {
				backend.relation.AffectedAccessKeys = []string{"service-child"}
			}
			plan, err := prepareIAMUserMutation(context.Background(), actor, args)
			if scenario == "capability" || scenario == "missing-parent" {
				if err == nil {
					t.Fatal("unknown relation accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "actor":
				actor.nativeUser = "bob"
			case "identity":
				actor.runtime.identity = "changed"
			case "lifetime":
				actor.runtime.lifetime = &backendLifetime{}
			case "namespace":
				backend.target = "other-storage"
			case "backend":
				actor.runtime.backend = &userMutationPlanTestBackend{Backend: backend}
			case "expired":
				plan.expires = time.Now().Add(-time.Second)
			}
			w := httptest.NewRecorder()
			s.executeIAMUserMutation(w, httptest.NewRequest(http.MethodPost, "/", nil), actor, plan)
			if w.Code != 409 || backend.writes.Load() != 0 {
				t.Fatalf("stale plan dispatched: %d", w.Code)
			}
		})
	}
}
