// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
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
				s.ServeHTTP(httptest.NewRecorder(), testRequest("GET", "/api/diagnostics/server", "", cookie))
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

func TestDiagnosticsNativeRouteVerifiesCurrentIdentity(t *testing.T) {
	var denied atomic.Bool
	var verified, reads atomic.Int32
	s := nativeFixture(t, func(_ context.Context, c NativeCredentials) (NativeConnection, error) {
		b := &diagnosticBackend{read: func(context.Context) (consoleapi.ServerDiagnostics, error) {
			reads.Add(1)
			return consoleapi.ServerDiagnostics{Nodes: []consoleapi.DiagnosticNode{{Version: "release"}}}, nil
		}}
		return NativeConnection{Backend: b, Cleanup: func() {}, Identity: fmt.Sprintf("%x", sha256.Sum256([]byte(c.AccessKey))), Verify: func(context.Context) error {
			verified.Add(1)
			if denied.Load() {
				return &consoleapi.Error{Status: 403, Code: "AccessDenied"}
			}
			return nil
		}}, nil
	})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/diagnostics/server", "", nil))
	if w.Code != 401 || reads.Load() != 0 || verified.Load() != 0 {
		t.Fatal("unauthenticated diagnostic dispatch")
	}
	cookie, _ := nativeSignIn(t, s, "alice")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/diagnostics/server", "", cookie))
	if w.Code != 200 || reads.Load() != 1 || verified.Load() != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("native diagnostics skipped verification: %d", w.Code)
	}
	denied.Store(true)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/diagnostics/server", "", cookie))
	if w.Code != 401 || reads.Load() != 1 || verified.Load() != 2 {
		t.Fatal("revoked identity reached diagnostics")
	}
}
