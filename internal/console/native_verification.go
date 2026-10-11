// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

// Every new API request checks the session's own signed credentials. No shared
// cached verdict can extend an old secret's authority after an external change.
// Caller holds a client lease; verification uses bounded global concurrency.
func (s *Server) verifyNativeSession(w http.ResponseWriter, r *http.Request, sess *session) bool {
	if err := tryAcquire(r.Context(), s.verificationSlots); err != nil {
		writeSlotError(w, err)
		return false
	}
	defer func() { <-s.verificationSlots }()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	stop := context.AfterFunc(sess.ctx, cancel)
	defer func() { stop(); cancel() }()
	err := sess.runtime.verify(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		return true
	}
	var failure *consoleapi.Error
	if errors.As(err, &failure) && failure != nil && failure.Code == "AccessDenied" {
		_, token := s.authenticate(r)
		s.mu.Lock()
		s.removeSessionLocked(token, sess)
		s.mu.Unlock()
		http.SetCookie(w, s.sessionCookie("", -1))
		writeError(w, 401, "login_required", s.loginPrompt())
		return false
	}
	// Outages or unsupported protocol do not prove revocation. Keep the session
	// but do not dispatch its storage operation until identity can be verified.
	writeError(w, 503, "identity_unavailable", "Unable to verify your identity. Try again when the storage service is available.")
	return false
}
