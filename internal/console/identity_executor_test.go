// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestIdentityExecutorUnknownOrMixedImpactHasNoSideEffects(t *testing.T) {
	s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
	for _, impact := range []identityImpact{{}, {known: true}, {known: true, scopes: []identityScope{nativeUserScope("alice"), {}}}} {
		w := httptest.NewRecorder()
		if s.beginIdentityMutation(w, nil, impact) != nil || w.Code != 409 || len(s.writeSlots) != 0 || len(s.nativeUserChanging) != 0 {
			t.Fatal("unknown impact acquired barriers")
		}
	}
}

func TestIdentityExecutorAtomicBarrierAndIdempotentRelease(t *testing.T) {
	s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, nil)
	alice, _ := nativeSignIn(t, s, "alice")
	bob, _ := nativeSignIn(t, s, "bob")
	var sess *session
	s.mu.Lock()
	for _, candidate := range s.sessions {
		if candidate.nativeUser == "alice" {
			sess = candidate
		}
	}
	s.mu.Unlock()
	impact := directIdentityImpact(nativeUserScope("alice"))
	// A storage writer (including multipart cleanup) prevents identity dispatch.
	s.writeSlots <- struct{}{}
	w := httptest.NewRecorder()
	if s.beginIdentityMutation(w, sess, impact) != nil || w.Code != 409 || len(s.writeSlots) != 1 || len(s.nativeUserChanging) != 0 {
		t.Fatal("partial reservation leaked or mutation admitted active writer")
	}
	<-s.writeSlots
	mutation := s.beginIdentityMutation(httptest.NewRecorder(), sess, impact)
	if mutation == nil {
		t.Fatal("no reservation")
	}
	if nativeSessionStatus(s, alice) != 409 || nativeSessionStatus(s, bob) != 200 {
		t.Fatal("target request gate leaked")
	}
	mutation.finish(false)
	mutation.finish(true) // Repeated finish cannot revoke after a known rejection.
	if nativeSessionStatus(s, alice) != 200 || len(s.writeSlots) != 0 || len(s.nativeUserChanging) != 0 {
		t.Fatal("rejected mutation retired session or leaked reservation")
	}
	mutation = s.beginIdentityMutation(httptest.NewRecorder(), sess, impact)
	if mutation == nil {
		t.Fatal("reservation after rejection failed")
	}
	mutation.finish(true)
	mutation.finish(true)
	if nativeSessionStatus(s, alice) != 401 || nativeSessionStatus(s, bob) != 200 || len(s.writeSlots) != 0 {
		t.Fatal("retirement was not scoped and idempotent")
	}
}
