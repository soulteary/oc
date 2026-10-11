// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soulteary/mc/internal/consoleapi"
)

func nativeDisableFixture(t *testing.T, action func(context.Context, consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error), login func(string)) *Server {
	t.Helper()
	s, err := New(Config{Alias: "storage", BaseURL: nativeOrigin, AllowWrites: true, NativeLogin: func(_ context.Context, c NativeCredentials) (NativeConnection, error) {
		if login != nil {
			login(c.AccessKey)
		}
		b := &fakeIAMBackend{fakeMutationBackend: &fakeMutationBackend{fakeBackend: &fakeBackend{}}, action: action}
		return NativeConnection{Backend: b, Cleanup: func() {}, Verify: func(context.Context) error { return nil }, Identity: fmt.Sprintf("%x", sha256.Sum256([]byte(c.AccessKey)))}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drainOwnedClients(t, s) })
	return s
}
func nativeDisable(s *Server, cookie *http.Cookie, reply sessionReply, target string) *httptest.ResponseRecorder {
	r := nativeRequest("POST", "/api/iam/actions", fmt.Sprintf(`{"action":"user.disable","user":%q,"confirmTarget":%q}`, target, target), cookie)
	r.Header.Set("X-CSRF-Token", reply.CSRFToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func nativeSessionStatus(s *Server, cookie *http.Cookie) int {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/session", "", cookie))
	return w.Code
}
func TestNativeDisableRetiresExactTargetOnConfirmedOrUncertainOutcome(t *testing.T) {
	for _, outcome := range []string{"confirmed", "unknown", "raw-error", "inconsistent", "denied", "self", "self-unknown"} {
		t.Run(outcome, func(t *testing.T) {
			s := nativeDisableFixture(t, func(context.Context, consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error) {
				switch outcome {
				case "unknown", "self-unknown":
					return consoleapi.IAMActionResult{}, &consoleapi.Error{Status: 502, Code: "outcome_unknown"}
				case "raw-error":
					return consoleapi.IAMActionResult{}, errors.New("lost acknowledgement")
				case "inconsistent":
					return consoleapi.IAMActionResult{Outcome: "unknown"}, nil
				case "denied":
					return consoleapi.IAMActionResult{}, &consoleapi.Error{Status: 403, Code: "AccessDenied"}
				}
				return consoleapi.IAMActionResult{Outcome: "confirmed", RestartRequired: true}, nil
			}, nil)
			alice, ar := nativeSignIn(t, s, "alice")
			other, _ := nativeSignIn(t, s, "alice")
			bob, br := nativeSignIn(t, s, "bob")
			operator, reply := bob, br
			if outcome == "self" || outcome == "self-unknown" {
				operator, reply = alice, ar
			}
			w := nativeDisable(s, operator, reply, "alice")
			want := 200
			if outcome == "denied" {
				want = 403
			} else if outcome == "unknown" || outcome == "self-unknown" || outcome == "raw-error" || outcome == "inconsistent" {
				want = 502
			}
			if w.Code != want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			targetStatus := 401
			if outcome == "denied" {
				targetStatus = 200
			}
			for _, cookie := range []*http.Cookie{alice, other} {
				if got := nativeSessionStatus(s, cookie); got != targetStatus {
					t.Fatalf("target session %d want %d", got, targetStatus)
				}
			}
			if nativeSessionStatus(s, bob) != 200 {
				t.Fatal("unrelated session ended")
			}
			if len(s.writeSlots) != 0 || len(s.nativeUserChanging) != 0 {
				t.Fatal("reservation leaked")
			}
			select {
			case <-s.Done():
				t.Fatal("shared console stopped")
			default:
			}
		})
	}
}
func TestNativeDisableQuiescesWritesAndGatesOnlyTarget(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s := nativeDisableFixture(t, func(context.Context, consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error) {
		close(entered)
		<-release
		return consoleapi.IAMActionResult{Outcome: "confirmed"}, nil
	}, nil)
	alice, _ := nativeSignIn(t, s, "alice")
	bob, br := nativeSignIn(t, s, "bob")
	s.writeSlots <- struct{}{}
	if nativeDisable(s, bob, br, "alice").Code != 409 {
		t.Fatal("active write accepted")
	}
	<-s.writeSlots
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- nativeDisable(s, bob, br, "alice") }()
	<-entered
	if nativeSessionStatus(s, alice) != 409 || nativeSessionStatus(s, bob) != 200 {
		t.Fatal("target gate affects wrong session")
	}
	nativeSignIn(t, s, "bob")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("POST", "/api/login", `{"accessKey":"alice","secretKey":"native-secret"}`, nil))
	if w.Code != 409 {
		t.Fatal("target login accepted")
	}
	close(release)
	if (<-done).Code != 200 {
		t.Fatal("disable failed")
	}
}
func TestNativeDisableRejectsPendingLoginEvenAfterAccountIsEnabledAgain(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	pending := false
	s := nativeDisableFixture(t, func(context.Context, consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error) {
		return consoleapi.IAMActionResult{Outcome: "confirmed"}, nil
	}, func(user string) {
		if user == "alice" && pending {
			close(entered)
			<-release
		}
	})
	bob, br := nativeSignIn(t, s, "bob")
	pending = true
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, nativeRequest("POST", "/api/login", `{"accessKey":"alice","secretKey":"native-secret"}`, nil))
		done <- w.Code
	}()
	<-entered
	if nativeDisable(s, bob, br, "alice").Code != 200 {
		t.Fatal("disable failed")
	}
	close(release)
	if <-done != 409 {
		t.Fatal("stale verified login committed")
	}
	if len(s.nativeUserRevoked) != 0 {
		t.Fatal("tombstone retained without pending login")
	}
}

func TestNativeSelfDisableRetiresAfterAcknowledgementEvenWhenFlushFails(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprint(broken), func(t *testing.T) {
			s := nativeDisableFixture(t, func(context.Context, consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error) {
				return consoleapi.IAMActionResult{Outcome: "confirmed", RestartRequired: true}, nil
			}, nil)
			alice, ar := nativeSignIn(t, s, "alice")
			sess, _ := s.authenticate(nativeRequest("GET", "/api/session", "", alice))
			r := nativeRequest("POST", "/api/iam/actions", `{"action":"user.disable","user":"alice","confirmTarget":"alice"}`, alice)
			r.Header.Set("X-CSRF-Token", ar.CSRFToken)
			w := &disableFlushWriter{ResponseRecorder: httptest.NewRecorder(), onFlush: func() {
				if sess.ctx.Err() != nil {
					t.Error("self session retired before acknowledgement")
				}
				if len(s.writeSlots) != cap(s.writeSlots) {
					t.Error("write reservation released before self acknowledgement")
				}
			}, broken: broken}
			func() {
				defer func() {
					if recovered := recover(); recovered != nil && recovered != http.ErrAbortHandler {
						t.Errorf("unexpected panic: %v", recovered)
					}
				}()
				s.ServeHTTP(w, r)
			}()
			if sess.ctx.Err() == nil || nativeSessionStatus(s, alice) != 401 {
				t.Fatal("self session survives acknowledgement/flush failure")
			}
			if len(s.writeSlots) != 0 {
				t.Fatal("write reservation leaked after flush failure")
			}
		})
	}
}

type disableFlushWriter struct {
	*httptest.ResponseRecorder
	onFlush func()
	broken  bool
}

func (w *disableFlushWriter) FlushError() error {
	w.onFlush()
	if w.broken {
		return errors.New("broken browser socket")
	}
	w.ResponseRecorder.Flush()
	return nil
}
