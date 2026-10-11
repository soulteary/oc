// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"net/http"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

// serveDiagnostics runs after the standard runtime lease and native verification.
func (s *Server) serveDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !s.requireMethod(w, r, http.MethodGet) {
		return
	}
	sess, _ := s.authenticate(r)
	if sess == nil {
		writeError(w, 401, "login_required", s.loginPrompt())
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_input", "Diagnostics do not accept query parameters.")
		return
	}
	backend, ok := sess.runtime.backend.(consoleapi.DiagnosticsBackend)
	if !ok {
		writeError(w, 501, "diagnostics_unsupported", "This connection does not support server diagnostics.")
		return
	}
	deadline := time.Now().Add(15 * time.Second)
	if sess.expires.Before(deadline) {
		deadline = sess.expires
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	stop := context.AfterFunc(sess.ctx, cancel)
	defer func() { stop(); cancel() }()
	if !acquire(w, ctx, s.apiSlots) {
		return
	}
	defer func() { <-s.apiSlots }()
	result, err := backend.ServerDiagnostics(ctx)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, result)
}
