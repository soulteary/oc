// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soulteary/mc/internal/consoleapi"
)

const nativeOrigin = "https://console.example"

func nativeRequest(method, path, body string, cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest(method, nativeOrigin+path, strings.NewReader(body))
	r.TLS = &tls.ConnectionState{}
	if method == "POST" || method == "PUT" {
		r.Header.Set("Origin", nativeOrigin)
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}
func nativeSignIn(t *testing.T, s *Server, user string) (*http.Cookie, sessionReply) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"accessKey": user, "secretKey": "native-secret"})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("POST", "/api/login", string(body), nil))
	if w.Code != 200 {
		t.Fatalf("native login failed: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("native login did not set one cookie")
	}
	var reply sessionReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	return cookies[0], reply
}
func nativeFixture(t *testing.T, authenticate func(context.Context, NativeCredentials) (NativeConnection, error)) *Server {
	t.Helper()
	s, err := New(Config{NativeLogin: authenticate, Alias: "storage", BaseURL: nativeOrigin, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drainOwnedClients(t, s) })
	return s
}
func nativeFixtureAuthenticator(closed *atomic.Int32) func(context.Context, NativeCredentials) (NativeConnection, error) {
	return func(ctx context.Context, c NativeCredentials) (NativeConnection, error) {
		user := c.AccessKey
		backend := &fakeBackend{list: func(context.Context) ([]consoleapi.Bucket, error) {
			return []consoleapi.Bucket{{Name: user + "-bucket"}}, nil
		}}
		return NativeConnection{Backend: backend, Cleanup: func() { closed.Add(1) }, Identity: fmt.Sprintf("%x", sha256.Sum256([]byte("fixed-target/native/"+user)))}, nil
	}
}

func TestNativeModeRequiresDedicatedHTTPSReadOnlyAuthentication(t *testing.T) {
	var closed atomic.Int32
	auth := nativeFixtureAuthenticator(&closed)
	for _, change := range []func(*Config){
		func(c *Config) { c.BaseURL = "http://127.0.0.1:9090" },
		func(c *Config) { c.BaseURL = "https://0.0.0.0:9090" },
		func(c *Config) { c.Backend = &fakeBackend{} },
		func(c *Config) { c.LoginCode = "shared-code" },
		func(c *Config) { c.AllowWrites = true },
		func(c *Config) {
			c.BackendFactory = func(context.Context) (consoleapi.Backend, func(), error) { return nil, nil, nil }
		},
	} {
		cfg := Config{NativeLogin: auth, Alias: "storage", BaseURL: nativeOrigin}
		change(&cfg)
		if s, err := New(cfg); err == nil {
			_ = s.Close()
			t.Fatal("unsafe native configuration accepted")
		}
	}
}

func TestNativeSessionsIsolateConnectionsPreferencesAndLogout(t *testing.T) {
	var closed atomic.Int32
	s := nativeFixture(t, nativeFixtureAuthenticator(&closed))
	alice, aliceReply := nativeSignIn(t, s, "alice")
	aliceAgain, _ := nativeSignIn(t, s, "alice")
	bob, bobReply := nativeSignIn(t, s, "bob")
	if alice.Name != nativeCookieName || !alice.Secure || !alice.HttpOnly || alice.Path != "/" || alice.Domain != "" || alice.SameSite != http.SameSiteLaxMode || !aliceReply.ReadOnly {
		t.Fatal("native session cookie or write gate was unsafe")
	}
	update := nativeRequest("PUT", "/api/preferences", `{"action":"language","language":"en"}`, alice)
	update.Header.Set("X-CSRF-Token", aliceReply.CSRFToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, update)
	if w.Code != 200 {
		t.Fatalf("preferences failed: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		cookie           *http.Cookie
		bucket, language string
	}{{alice, "alice-bucket", "en"}, {aliceAgain, "alice-bucket", "en"}, {bob, "bob-bucket", "zh"}} {
		w = httptest.NewRecorder()
		s.ServeHTTP(w, nativeRequest("GET", "/api/buckets", "", tc.cookie))
		if w.Code != 200 || !strings.Contains(w.Body.String(), tc.bucket) {
			t.Fatal("native connections leaked between users")
		}
		w = httptest.NewRecorder()
		s.ServeHTTP(w, nativeRequest("GET", "/api/preferences", "", tc.cookie))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"language":"`+tc.language+`"`) {
			t.Fatalf("preferences leaked: %s", w.Body.String())
		}
	}
	logout := nativeRequest("POST", "/api/logout", "", alice)
	logout.Header.Set("X-CSRF-Token", aliceReply.CSRFToken)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, logout)
	if w.Code != 204 || !w.Result().Cookies()[0].Secure || w.Result().Cookies()[0].Name != nativeCookieName {
		t.Fatal("native logout did not delete its secure cookie")
	}
	for _, cookie := range []*http.Cookie{aliceAgain, bob} {
		w = httptest.NewRecorder()
		s.ServeHTTP(w, nativeRequest("GET", "/api/buckets", "", cookie))
		if w.Code != 200 {
			t.Fatal("logout revoked another browser")
		}
	}
	for _, path := range []string{"/api/uploads", "/api/buckets/create", "/api/iam/actions", "/api/self-secret"} {
		r := nativeRequest("POST", path, `{}`, bob)
		r.Header.Set("X-CSRF-Token", bobReply.CSRFToken)
		w = httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("shared write was accepted at %s: %d", path, w.Code)
		}
	}
	drainOwnedClients(t, s)
	if closed.Load() != 3 {
		t.Fatal("native session clients leaked")
	}
}

func TestNativeLoginRejectsInsecureTransportAndLegacyCode(t *testing.T) {
	var calls atomic.Int32
	s := nativeFixture(t, func(context.Context, NativeCredentials) (NativeConnection, error) {
		calls.Add(1)
		return NativeConnection{}, nil
	})
	for _, path := range []string{"/", "/api/login"} {
		r := nativeRequest("POST", path, `{"accessKey":"alice","secretKey":"native-secret"}`, nil)
		r.TLS = nil
		r.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("native transport trusted a forwarded header")
		}
	}
	for _, body := range []string{`{"code":"local-code"}`, `{"accessKey":"alice","secretKey":"native-secret","endpoint":"https://attacker.example"}`, `{"accessKey":"alice","secretKey":"native-secret","sessionToken":"sts"}`, `{"accessKey":"alice","secretKey":"short"}`} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, nativeRequest("POST", "/api/login", body, nil))
		if w.Code < 400 {
			t.Fatal("unsafe native request accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected login reached the authenticator")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("GET", "/", "", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `data-auth-mode="native"`) {
		t.Fatal("HTTPS page did not select native login")
	}
}

func TestNativePrincipalSessionLimitDoesNotBlockAnotherUser(t *testing.T) {
	var closed atomic.Int32
	s := nativeFixture(t, nativeFixtureAuthenticator(&closed))
	for i := 0; i < 4; i++ {
		nativeSignIn(t, s, "alice")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, nativeRequest("POST", "/api/login", `{"accessKey":"alice","secretKey":"native-secret"}`, nil))
	if w.Code != 429 || len(w.Result().Cookies()) != 0 {
		t.Fatal("one principal exhausted all session capacity")
	}
	nativeSignIn(t, s, "bob")
	drainOwnedClients(t, s)
	if closed.Load() != 6 {
		t.Fatal("rejected principal session leaked a client")
	}
}

func TestNativeReloginKeepsPreferenceStoreUntilRetiredRequestsDrain(t *testing.T) {
	var closed atomic.Int32
	s := nativeFixture(t, nativeFixtureAuthenticator(&closed))
	cookie, reply := nativeSignIn(t, s, "alice")
	sess, _ := s.authenticate(nativeRequest("GET", "/api/session", "", cookie))
	if !sess.runtime.retain() {
		t.Fatal("unable to retain request")
	}
	defer sess.runtime.release()
	logout := nativeRequest("POST", "/api/logout", "", cookie)
	logout.Header.Set("X-CSRF-Token", reply.CSRFToken)
	s.ServeHTTP(httptest.NewRecorder(), logout)
	nextCookie, _ := nativeSignIn(t, s, "alice")
	next, _ := s.authenticate(nativeRequest("GET", "/api/session", "", nextCookie))
	if next.runtime.preferences != sess.runtime.preferences {
		t.Fatal("re-login forked preferences while a revoked request was still finishing")
	}
}
