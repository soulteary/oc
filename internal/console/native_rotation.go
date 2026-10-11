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
	mutation := s.beginIdentityMutation(w, sess, directIdentityImpact(nativePrincipalScope(sess.runtime.identity)))
	if mutation == nil {
		return
	}
	revoke := false
	defer func() { mutation.finish(revoke) }()
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
