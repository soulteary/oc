// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"encoding/hex"
	"errors"

	"github.com/soulteary/mc/internal/consoleapi"
)

// identityScope has separate namespaces for a native connection principal and
// its authenticated IAM user. Untrusted strings never become another kind.
// OIDC issuer/subject and IAM parent mapping are deliberately not represented.
type identityScope struct {
	kind uint8
	key  string
}

const (
	nativePrincipalScopeKind uint8 = iota + 1
	nativeUserScopeKind
)

func nativePrincipalScope(key string) identityScope {
	return identityScope{nativePrincipalScopeKind, key}
}
func nativeUserScope(key string) identityScope { return identityScope{nativeUserScopeKind, key} }
func (scope identityScope) valid() bool {
	switch scope.kind {
	case nativePrincipalScopeKind:
		_, err := hex.DecodeString(scope.key)
		return len(scope.key) == 64 && err == nil
	case nativeUserScopeKind:
		return consoleapi.ValidIAMAccessKey(scope.key)
	default:
		return false
	}
}

var errUnknownIdentityScope = errors.New("identity impact scope is unknown")

// identityRevocations is owned by Server; EVERY method/field access requires
// Server.mu. It records only changes that could overlap an in-flight login.
// Active sessions are retired by the caller, never resurrected by this ledger.
// A known scope must be validated before dispatching an identity mutation.
type identityRevocations struct {
	epoch      uint64
	pending    int
	tombstones map[identityScope]uint64
}

func (state *identityRevocations) begin() uint64 {
	state.pending++
	return state.epoch
}
func (state *identityRevocations) finish() {
	state.pending--
	if state.pending == 0 {
		clear(state.tombstones)
	}
}
func (state *identityRevocations) revoke(scope identityScope) error {
	if !scope.valid() {
		return errUnknownIdentityScope
	}
	state.epoch++
	if state.pending > 0 {
		if state.tombstones == nil {
			state.tombstones = make(map[identityScope]uint64)
		}
		state.tombstones[scope] = state.epoch
	}
	return nil
}
func (state *identityRevocations) changedSince(scope identityScope, epoch uint64) bool {
	return !scope.valid() || state.tombstones[scope] > epoch
}
