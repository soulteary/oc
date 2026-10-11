// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"errors"
	"net/http"

	"github.com/soulteary/mc/internal/consoleapi"
)

// Reserve the existing global write budget before changing credentials. Active
// writes (including multipart abort) must finish first. Other users' reads and
// logins remain available; competing writes receive the ordinary busy response.
func (s *Server) rotateNativeSecret(w http.ResponseWriter, r *http.Request, sess *session, secret string) {
	if !nativePrincipalScope(sess.runtime.identity).valid() {
		writeError(w, 409, "identity_impact_unknown", "The affected identity cannot be established safely.")
		return
	}
	s.mu.Lock()
	if s.closed || sess.ctx.Err() != nil || s.nativeRotating[sess.runtime.identity] || s.nativeUserChanging[sess.nativeUser] {
		s.mu.Unlock()
		writeError(w, 409, "credential_change_pending", "This session is ending or its secret is being changed.")
		return
	}
	for i := 0; i < cap(s.writeSlots); i++ {
		if err := tryAcquire(sess.ctx, s.writeSlots); err != nil {
			for j := 0; j < i; j++ {
				<-s.writeSlots
			}
			s.mu.Unlock()
			writeError(w, 409, "active_tasks", "Complete or cancel active storage writes before changing the account secret.")
			return
		}
	}
	s.nativeRotating[sess.runtime.identity] = true
	s.workers.Add(1)
	s.mu.Unlock()
	revoke := false
	defer func() {
		s.mu.Lock()
		if revoke {
			_ = s.revocations.revoke(nativePrincipalScope(sess.runtime.identity))
			for token, other := range s.sessions {
				if other.runtime.identity == sess.runtime.identity {
					s.removeSessionLocked(token, other)
				}
			}
		}
		delete(s.nativeRotating, sess.runtime.identity)
		for i := 0; i < cap(s.writeSlots); i++ {
			<-s.writeSlots
		}
		s.mu.Unlock()
		s.workers.Done()
	}()
	err := sess.runtime.settings.RotateOwnSecret(r.Context(), secret)
	if err != nil {
		var apiError *consoleapi.Error
		if errors.As(err, &apiError) && apiError != nil && apiError.Code != "outcome_unknown" {
			status := apiError.Status
			if status < 400 || status > 599 {
				status = 502
			}
			writeJSON(w, status, struct {
				*consoleapi.Error
				RestartRequired bool `json:"restartRequired"`
			}{apiError, false})
			return
		}
		revoke = true
		writeJSON(w, 502, struct {
			Code          string `json:"code"`
			Message       string `json:"message"`
			LoginRequired bool   `json:"loginRequired"`
		}{"outcome_unknown", "The secret may have changed. Verify the current secret before signing in again.", true})
		return
	}
	revoke = true
	writeJSON(w, 200, struct {
		Outcome       string `json:"outcome"`
		LoginRequired bool   `json:"loginRequired"`
	}{"confirmed", true})
}
