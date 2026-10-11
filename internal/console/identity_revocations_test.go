// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

// Unit instances are exclusively owned; production instances require Server.mu.
func TestIdentityRevocationsRejectUnknownScope(t *testing.T) {
	var state identityRevocations
	epoch := state.begin()
	for _, scope := range []identityScope{{}, {kind: 99, key: "alice"}, nativePrincipalScope("alice"), nativeUserScope("")} {
		if state.revoke(scope) != errUnknownIdentityScope || !state.changedSince(scope, epoch) {
			t.Fatal("unknown scope admitted")
		}
	}
	if state.epoch != epoch || len(state.tombstones) != 0 {
		t.Fatal("unknown scope changed ledger")
	}
	state.finish()
}
func TestIdentityRevocationsScopeIsolationAndRepeatedChanges(t *testing.T) {
	var state identityRevocations
	// Equal textual keys must still be isolated by scope kind.
	key := strings.Repeat("a", 64)
	principal, user := nativePrincipalScope(key), nativeUserScope(key)
	before := state.begin()
	if err := state.revoke(principal); err != nil {
		t.Fatal(err)
	}
	if !state.changedSince(principal, before) || state.changedSince(user, before) || state.changedSince(nativeUserScope("bob"), before) {
		t.Fatal("scope namespace leaked")
	}
	between := state.begin()
	if state.changedSince(principal, between) {
		t.Fatal("fresh login treated as stale")
	}
	if err := state.revoke(principal); err != nil {
		t.Fatal(err)
	}
	if !state.changedSince(principal, between) || len(state.tombstones) != 1 {
		t.Fatal("repeat revoke lost overlapping login or grew tombstones")
	}
	after := state.begin()
	if state.changedSince(principal, after) {
		t.Fatal("post-change login rejected")
	}
	state.finish()
	state.finish()
	if len(state.tombstones) != 1 || !state.changedSince(principal, before) {
		t.Fatal("pending login protection discarded early")
	}
	state.finish()
	if state.pending != 0 || len(state.tombstones) != 0 {
		t.Fatal("tombstones retained without pending login")
	}
	if err := state.revoke(user); err != nil {
		t.Fatal(err)
	}
	if len(state.tombstones) != 0 {
		t.Fatal("idle revoke retained tombstone")
	}
	fresh := state.begin()
	if state.changedSince(user, fresh) || state.changedSince(principal, fresh) {
		t.Fatal("fresh login cannot commit after collected revoke")
	}
	state.finish()
}

func TestUnknownIdentityScopeRejectsBeforeMutationOrWriteReservation(t *testing.T) {
	s := testServer(t, &fakeBackend{}, time.Hour)
	w := httptest.NewRecorder()
	s.retireNativeUser(w, httptest.NewRequest("POST", "/api/iam/actions", nil), nil, nil, consoleapi.IAMActionRequest{Action: "user.delete"})
	if w.Code != 409 || len(s.writeSlots) != 0 {
		t.Fatal("unknown user reached mutation reservation")
	}
	w = httptest.NewRecorder()
	s.rotateNativeSecret(w, httptest.NewRequest("POST", "/api/self-secret", nil), &session{}, "new-secret")
	if w.Code != 409 || len(s.writeSlots) != 0 {
		t.Fatal("unknown principal reached mutation reservation")
	}
}
