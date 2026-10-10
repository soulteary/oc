// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soulteary/mc/internal/consoleapi"
)

func nativeRotationFixture(t *testing.T, rotate func(context.Context, string) error, loginGate func(NativeCredentials)) *Server {
	t.Helper()
	s, err := New(Config{Alias: "storage", BaseURL: nativeOrigin, AllowWrites: true, NativeLogin: func(_ context.Context, c NativeCredentials) (NativeConnection, error) {
		if loginGate != nil {
			loginGate(c)
		}
		b := &fakeSettingsBackend{fakeMutationBackend: &fakeMutationBackend{fakeBackend: &fakeBackend{}}, rotate: rotate}
		return NativeConnection{Backend: b, Cleanup: func() {}, Identity: fmt.Sprintf("%x", sha256.Sum256([]byte(c.AccessKey)))}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drainOwnedClients(t, s) })
	return s
}
func nativeRotate(s *Server, cookie *http.Cookie, reply sessionReply) *httptest.ResponseRecorder {
	r := nativeRequest("POST", "/api/self-secret", `{"newSecret":"changed-secret","confirm":true}`, cookie)
	r.Header.Set("X-CSRF-Token", reply.CSRFToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestNativeRotationRetiresOnlyOwnSessions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		revoke bool
	}{
		{"confirmed", nil, 200, true}, {"uncertain", &consoleapi.Error{Status: 502, Code: "outcome_unknown"}, 502, true}, {"denied", &consoleapi.Error{Status: 403, Code: "AccessDenied"}, 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := nativeRotationFixture(t, func(context.Context, string) error { return tc.err }, nil)
			alice, ar := nativeSignIn(t, s, "alice")
			second, _ := nativeSignIn(t, s, "alice")
			bob, _ := nativeSignIn(t, s, "bob")
			if w := nativeRotate(s, alice, ar); w.Code != tc.status {
				t.Fatalf("rotation: %d %s", w.Code, w.Body.String())
			}
			for _, cookie := range []*http.Cookie{alice, second, bob} {
				w := httptest.NewRecorder()
				s.ServeHTTP(w, nativeRequest("GET", "/api/session", "", cookie))
				want := 200
				if cookie != bob && tc.revoke {
					want = 401
				}
				if w.Code != want {
					t.Fatalf("session status %d want %d", w.Code, want)
				}
			}
			select {
			case <-s.Done():
				t.Fatal("rotation stopped shared server")
			default:
			}
			nativeSignIn(t, s, "alice")
		})
	}
}
func TestNativeRotationRejectsLoginValidatedBeforeRotation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s := nativeRotationFixture(t, func(context.Context, string) error { return nil }, func(c NativeCredentials) {
		if c.SecretKey == "pending-secret" {
			close(started)
			<-release
		}
	})
	alice, ar := nativeSignIn(t, s, "alice")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, nativeRequest("POST", "/api/login", `{"accessKey":"alice","secretKey":"pending-secret"}`, nil))
		done <- w
	}()
	<-started
	if w := nativeRotate(s, alice, ar); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	close(release)
	w := <-done
	if w.Code != 409 || len(w.Result().Cookies()) != 0 {
		t.Fatal("stale login survived rotation")
	}
	s.mu.Lock()
	n := len(s.nativeRevoked)
	s.mu.Unlock()
	if n != 0 {
		t.Fatal("rotation tombstones retained without pending logins")
	}
}
func TestNativeRotationRejectsActiveWritesWithoutRevoking(t *testing.T) {
	s := nativeRotationFixture(t, func(context.Context, string) error { t.Fatal("rotation called during write"); return nil }, nil)
	alice, ar := nativeSignIn(t, s, "alice")
	s.writeSlots <- struct{}{}
	w := nativeRotate(s, alice, ar)
	<-s.writeSlots
	if w.Code != 409 {
		t.Fatal("rotation accepted an active write")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/session", "", alice))
	if w.Code != 200 {
		t.Fatal("busy rotation revoked session")
	}
}

func TestNativeRotationDoesNotPauseOtherUsersReadsOrLogin(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s := nativeRotationFixture(t, func(context.Context, string) error { close(started); <-release; return nil }, nil)
	alice, ar := nativeSignIn(t, s, "alice")
	bob, _ := nativeSignIn(t, s, "bob")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- nativeRotate(s, alice, ar) }()
	<-started
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/session", "", bob))
	if w.Code != 200 {
		t.Fatal("rotation paused another user")
	}
	nativeSignIn(t, s, "bob")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/session", "", alice))
	if w.Code != 409 {
		t.Fatal("rotating user was not paused")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("POST", "/api/login", `{"accessKey":"alice","secretKey":"native-secret"}`, nil))
	if w.Code != 409 {
		t.Fatal("login accepted during rotation")
	}
	close(release)
	if w := <-done; w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}
