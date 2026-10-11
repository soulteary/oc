// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"testing"
	"time"
)

func TestOIDCPrincipalNamespace(t *testing.T) {
	scope, err := oidcPrincipalScope("s3\x00admin", "https://issuer", "sub")
	if err != nil || !scope.valid() {
		t.Fatal("valid verified principal rejected")
	}
	native := nativePrincipalScope(scope.key)
	if native == scope {
		t.Fatal("OIDC aliases native principal")
	}
	for _, tuple := range [][3]string{{"other", "https://issuer", "sub"}, {"s3\x00admin", "https://other", "sub"}, {"s3\x00admin", "https://issuer", "other"}} {
		key, err := OIDCPrincipalIdentity(tuple[0], tuple[1], tuple[2])
		if err != nil || key == scope.key {
			t.Fatal("principal namespaces overlap")
		}
	}
	for _, tuple := range [][3]string{{"", "issuer", "sub"}, {"target", "", "sub"}, {"target", "issuer", ""}, {"target", "issuer", "sub\x00"}} {
		if _, err := OIDCPrincipalIdentity(tuple[0], tuple[1], tuple[2]); err == nil {
			t.Fatal("invalid principal accepted")
		}
	}
	var ledger identityRevocations
	epoch := ledger.begin()
	defer ledger.finish()
	if err := ledger.revoke(scope); err != nil || !ledger.changedSince(scope, epoch) || ledger.changedSince(native, epoch) {
		t.Fatal("revocation crossed identity namespace")
	}
}
func TestIdentityRelationshipFailClosed(t *testing.T) {
	now := time.Now()
	snapshot := identityRelationshipSnapshot{target: "target", revision: "revision", expires: now.Add(time.Minute), state: identityRelationshipKnown}
	empty, err := snapshot.impact("target", "revision", now)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("known empty not preserved")
	}
	for _, mutate := range []func(*identityRelationshipSnapshot){func(s *identityRelationshipSnapshot) { s.state = identityRelationshipUnknown }, func(s *identityRelationshipSnapshot) { s.state = identityRelationshipExpired }, func(s *identityRelationshipSnapshot) { s.expires = now }, func(s *identityRelationshipSnapshot) { s.scopes = []identityScope{{}} }} {
		copy := snapshot
		mutate(&copy)
		if _, err := copy.impact("target", "revision", now); err == nil {
			t.Fatal("unknown/stale relationship accepted")
		}
	}
	if _, err := snapshot.impact("other", "revision", now); err == nil {
		t.Fatal("target mismatch accepted")
	}
	if _, err := snapshot.impact("target", "other", now); err == nil {
		t.Fatal("revision conflict accepted")
	}
	snapshot.scopes = []identityScope{nativeUserScope("native-user"), nativeUserScope("native-user")}
	scopes, err := snapshot.impact("target", "revision", now)
	if err != nil || len(scopes) != 1 {
		t.Fatal("valid impact rejected")
	}
	scopes[0] = identityScope{}
	if _, err := snapshot.impact("target", "revision", now); err != nil {
		t.Fatal("caller mutated authority")
	}
}
