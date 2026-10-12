// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import "net/http"

// identityImpact is an exact, locally established impact set. Relationship
// snapshots from list/get APIs cannot establish this set for parent or group
// mutations: their authorization can change before the mutation commits.
type identityImpact struct {
	scopes []identityScope
	known  bool
}

func directIdentityImpact(scope identityScope) identityImpact {
	return identityImpact{scopes: []identityScope{scope}, known: scope.valid()}
}
func (impact identityImpact) valid() bool {
	if !impact.known || impact.scopes == nil {
		return false
	}
	for _, scope := range impact.scopes {
		if !scope.valid() {
			return false
		}
	}
	return true
}
func (impact identityImpact) matches(sess *session) bool {
	for _, scope := range impact.scopes {
		if scope.kind == nativePrincipalScopeKind && sess.runtime.identity == scope.key || scope.kind == nativeUserScopeKind && sess.nativeUser == scope.key {
			return true
		}
	}
	return false
}

// identityMutation owns the login/request barriers and all write slots until
// finish. Call finish after writing a self-target response; retire other targets
// before responding. A rejected mutation releases the barriers without revoking.
// It is exclusively owned by the request goroutine; finish is idempotent.
type identityMutation struct {
	server   *Server
	impact   identityImpact
	finished bool
}

func (s *Server) beginIdentityMutation(w http.ResponseWriter, sess *session, impact identityImpact) *identityMutation {
	// The executor owns the same immutable snapshot used to validate and gate.
	if !impact.valid() || sess == nil || sess.ctx == nil {
		writeError(w, 409, "identity_impact_unknown", "The affected identity cannot be established safely.")
		return nil
	}
	impact.scopes = append([]identityScope{}, impact.scopes...)
	s.mu.Lock()
	if s.closed || s.rotating || sess.ctx.Err() != nil || s.nativeRotating[sess.runtime.identity] || s.nativeUserChanging[sess.nativeUser] {
		s.mu.Unlock()
		writeError(w, 409, "credential_change_pending", "This session is ending or its identity is being changed.")
		return nil
	}
	for _, scope := range impact.scopes {
		if scope.kind == nativePrincipalScopeKind && s.nativeRotating[scope.key] || scope.kind == nativeUserScopeKind && s.nativeUserChanging[scope.key] {
			s.mu.Unlock()
			writeError(w, 409, "credential_change_pending", "The affected identity is already being changed.")
			return nil
		}
	}
	for i := 0; i < cap(s.writeSlots); i++ {
		if err := tryAcquire(sess.ctx, s.writeSlots); err != nil {
			for j := 0; j < i; j++ {
				<-s.writeSlots
			}
			s.mu.Unlock()
			writeError(w, 409, "active_tasks", "Complete or cancel active storage writes before changing an identity.")
			return nil
		}
	}
	for _, scope := range impact.scopes {
		if scope.kind == nativePrincipalScopeKind {
			s.nativeRotating[scope.key] = true
		} else {
			s.nativeUserChanging[scope.key] = true
		}
	}
	s.workers.Add(1)
	s.mu.Unlock()
	return &identityMutation{server: s, impact: impact}
}
func (mutation *identityMutation) finish(revoke bool) {
	if mutation.finished {
		return
	}
	mutation.finished = true
	s := mutation.server
	s.mu.Lock()
	if revoke {
		for _, scope := range mutation.impact.scopes {
			_ = s.revocations.revoke(scope)
		}
		for token, sess := range s.sessions {
			if mutation.impact.matches(sess) {
				s.removeSessionLocked(token, sess)
			}
		}
	}
	for _, scope := range mutation.impact.scopes {
		if scope.kind == nativePrincipalScopeKind {
			delete(s.nativeRotating, scope.key)
		} else {
			delete(s.nativeUserChanging, scope.key)
		}
	}
	for i := 0; i < cap(s.writeSlots); i++ {
		<-s.writeSlots
	}
	s.mu.Unlock()
	s.workers.Done()
}
