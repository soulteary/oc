// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soulteary/mc/internal/consoleapi"
)

func verificationFixture(t *testing.T, verify func(context.Context, NativeCredentials) error, reads *atomic.Int32) *Server {
	t.Helper()
	return nativeFixture(t, func(_ context.Context, c NativeCredentials) (NativeConnection, error) {
		b := &fakeBackend{list: func(context.Context) ([]consoleapi.Bucket, error) { reads.Add(1); return nil, nil }}
		return NativeConnection{Backend: b, Cleanup: func() {}, Identity: fmt.Sprintf("%x", sha256.Sum256([]byte(c.AccessKey))), Verify: func(ctx context.Context) error { return verify(ctx, c) }}, nil
	})
}
func TestNativeRevalidationRevokesOldCredentialsWithoutRetiringFreshSession(t *testing.T) {
	var revoked atomic.Bool
	var reads atomic.Int32
	s := verificationFixture(t, func(_ context.Context, c NativeCredentials) error {
		if revoked.Load() && c.AccessKey == "alice" && c.SecretKey != "fresh-secret" {
			return &consoleapi.Error{Status: 403, Code: "AccessDenied"}
		}
		return nil
	}, &reads)
	old, _ := nativeSignIn(t, s, "alice")
	bob, _ := nativeSignIn(t, s, "bob")
	revoked.Store(true)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("POST", "/api/login", `{"accessKey":"alice","secretKey":"fresh-secret"}`, nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	fresh := w.Result().Cookies()[0]
	w = httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/buckets", "", old))
	if w.Code != 401 || reads.Load() != 0 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("old credentials reached storage or retained cookie")
	}
	for _, cookie := range []*http.Cookie{fresh, bob} {
		w = httptest.NewRecorder()
		s.ServeHTTP(w, nativeRequest("GET", "/api/buckets", "", cookie))
		if w.Code != 200 {
			t.Fatal("revalidation retired unrelated credentials")
		}
	}
	if reads.Load() != 2 {
		t.Fatal("wrong storage request count")
	}
	select {
	case <-s.Done():
		t.Fatal("revalidation stopped console")
	default:
	}
}
func TestNativeVerificationOutageBlocksStorageButKeepsSession(t *testing.T) {
	var failing atomic.Bool
	var reads atomic.Int32
	s := verificationFixture(t, func(context.Context, NativeCredentials) error {
		if failing.Load() {
			return errors.New("private upstream detail")
		}
		return nil
	}, &reads)
	cookie, reply := nativeSignIn(t, s, "alice")
	failing.Store(true)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/buckets", "", cookie))
	if w.Code != 503 || reads.Load() != 0 || strings.Contains(w.Body.String(), "private") || len(w.Result().Cookies()) != 0 {
		t.Fatal("outage leaked details, dispatched storage or revoked session")
	}
	failing.Store(false)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/buckets", "", cookie))
	if w.Code != 200 {
		t.Fatal("session did not recover after outage")
	}
	failing.Store(true)
	r := nativeRequest("POST", "/api/logout", "", cookie)
	r.Header.Set("X-CSRF-Token", reply.CSRFToken)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("outage blocked logout")
	}
}
func TestNativeVerificationCapacityCannotBlockLogout(t *testing.T) {
	var reads atomic.Int32
	s := verificationFixture(t, func(context.Context, NativeCredentials) error { return nil }, &reads)
	cookie, reply := nativeSignIn(t, s, "alice")
	for i := 0; i < cap(s.verificationSlots); i++ {
		s.verificationSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(s.verificationSlots); i++ {
			<-s.verificationSlots
		}
	}()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/api/buckets", "", cookie))
	if w.Code != 429 || reads.Load() != 0 {
		t.Fatal("verification budget bypassed")
	}
	r := nativeRequest("POST", "/api/logout", "", cookie)
	r.Header.Set("X-CSRF-Token", reply.CSRFToken)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("busy verification blocked logout")
	}
}
func TestNativeConnectionRequiresRevalidation(t *testing.T) {
	var closed atomic.Int32
	s := nativeFixture(t, func(context.Context, NativeCredentials) (NativeConnection, error) {
		return NativeConnection{Backend: &fakeBackend{}, Cleanup: func() { closed.Add(1) }, Identity: strings.Repeat("a", 64)}, nil
	})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("POST", "/api/login", `{"accessKey":"alice","secretKey":"native-secret"}`, nil))
	if w.Code != 503 || len(w.Result().Cookies()) != 0 || closed.Load() != 1 {
		t.Fatal("unverifiable native session accepted or client leaked")
	}
}
