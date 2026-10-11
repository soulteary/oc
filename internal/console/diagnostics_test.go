// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"crypto/sha256"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type diagnosticBackend struct {
	fakeBackend
	read func(context.Context) (consoleapi.ServerDiagnostics, error)
}

func (b *diagnosticBackend) ServerDiagnostics(ctx context.Context) (consoleapi.ServerDiagnostics, error) {
	return b.read(ctx)
}

func TestDiagnosticsSessionCancellationAndExpiry(t *testing.T) {
	for _, action := range []string{"expiry", "logout", "close"} {
		t.Run(action, func(t *testing.T) {
			s := testServer(t, &fakeBackend{}, time.Hour)
			cookie, _ := signIn(t, s)
			sess, _ := s.authenticate(testRequest("GET", "/api/session", "", cookie))
			started := make(chan struct{})
			sess.runtime.backend = &diagnosticBackend{read: func(ctx context.Context) (consoleapi.ServerDiagnostics, error) {
				close(started)
				<-ctx.Done()
				return consoleapi.ServerDiagnostics{}, ctx.Err()
			}}
			if action == "expiry" {
				sess.expires = time.Now().Add(30 * time.Millisecond)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.serveDiagnostics(httptest.NewRecorder(), testRequest("GET", "/api/diagnostics/server", "", cookie))
			}()
			<-started
			switch action {
			case "logout":
				s.mu.Lock()
				s.removeSessionLocked(sha256.Sum256([]byte(cookie.Value)), sess)
				s.mu.Unlock()
			case "close":
				_ = s.Close()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("diagnostic handler retained canceled session")
			}
		})
	}
}
