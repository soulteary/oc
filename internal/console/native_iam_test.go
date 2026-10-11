// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/mc/internal/consoleapi"
)

func TestNativeIAMCreationUsesOwnPermissionsAndDoesNotStopSessions(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint(uncertain), func(t *testing.T) {
			s, err := New(Config{Alias: "storage", BaseURL: nativeOrigin, AllowWrites: true, NativeLogin: func(_ context.Context, c NativeCredentials) (NativeConnection, error) {
				user := c.AccessKey
				b := &fakeIAMBackend{fakeMutationBackend: &fakeMutationBackend{fakeBackend: &fakeBackend{}}, action: func(context.Context, consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error) {
					if user == "bob" {
						return consoleapi.IAMActionResult{}, &consoleapi.Error{Status: 403, Code: "AccessDenied"}
					}
					if uncertain {
						return consoleapi.IAMActionResult{}, &consoleapi.Error{Status: 502, Code: "outcome_unknown"}
					}
					return consoleapi.IAMActionResult{Outcome: "confirmed"}, nil
				}}
				return NativeConnection{Backend: b, Cleanup: func() {}, Verify: func(context.Context) error { return nil }, Identity: fmt.Sprintf("%x", sha256.Sum256([]byte(user)))}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { drainOwnedClients(t, s) })
			alice, ar := nativeSignIn(t, s, "alice")
			bob, br := nativeSignIn(t, s, "bob")
			for _, body := range []string{`{"action":"user.create","user":"created-user","confirmTarget":"created-user"}`, `{"action":"group.create","group":"created-group","confirmTarget":"created-group"}`, `{"action":"service-account.create","user":"alice","confirmTarget":"alice"}`} {
				for _, tc := range []struct {
					user        string
					cookieReply sessionReply
				}{{"alice", ar}, {"bob", br}} {
					cookie := alice
					if tc.user == "bob" {
						cookie = bob
					}
					r := nativeRequest("POST", "/api/iam/actions", body, cookie)
					r.Header.Set("X-CSRF-Token", tc.cookieReply.CSRFToken)
					w := httptest.NewRecorder()
					s.ServeHTTP(w, r)
					want := 200
					if uncertain {
						want = 502
					}
					if tc.user == "bob" {
						want = 403
					}
					if w.Code != want {
						t.Fatalf("creation status %d want %d: %s", w.Code, want, w.Body.String())
					}
				}
			}
			r := nativeRequest("POST", "/api/iam/actions", `{"action":"user.disable","user":"bob","confirmTarget":"bob"}`, alice)
			r.Header.Set("X-CSRF-Token", ar.CSRFToken)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != 403 || !strings.Contains(w.Body.String(), "identity_changes_disabled") {
				t.Fatal("native identity edits enabled")
			}
			for _, cookie := range []*http.Cookie{alice, bob} {
				w = httptest.NewRecorder()
				s.ServeHTTP(w, nativeRequest("GET", "/api/session", "", cookie))
				if w.Code != 200 {
					t.Fatal("creation retired another session")
				}
			}
			select {
			case <-s.Done():
				t.Fatal("creation stopped shared console")
			default:
			}
		})
	}
}
