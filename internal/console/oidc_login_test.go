package console

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/mc/internal/oidc"
)

func oidcTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{OIDC: &OIDCConfig{Client: &oidc.Client{}, StorageTarget: "https://storage\x00https://admin", Connect: func(context.Context, oidc.Credentials) (consoleapi.Backend, func(), error) {
		return &fakeBackend{}, func() {}, nil
	}}, Alias: "store", BaseURL: "https://console.example", SessionTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drainOwnedClients(t, s) })
	return s
}
func oidcRequest(path string) *http.Request {
	r := httptest.NewRequest("GET", "https://console.example"+path, nil)
	r.TLS = &tls.ConnectionState{}
	return r
}

func TestOIDCCallbackBoundaryRejectsMalformedAndCrossSiteFetch(t *testing.T) {
	s := oidcTestServer(t)
	for _, path := range []string{"/api/oidc/callback?state=a&state=b&code=c", "/api/oidc/callback?state=a&code=b&error=denied", "/api/oidc/callback?state=a&code=b", "/api/oidc/callback?state=a&code=b&target=evil"} {
		w := httptest.NewRecorder()
		r := oidcRequest(path)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.Header.Set("Sec-Fetch-Dest", "document")
		s.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("malformed callback accepted: %s: %d", path, w.Code)
		}
	}
	for _, kind := range []string{"host", "origin", "fetch", "http"} {
		r := oidcRequest("/api/oidc/callback?state=a&code=b")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		switch kind {
		case "host":
			r.Host = "evil.example"
		case "origin":
			r.Header.Set("Origin", "https://evil.example")
		case "fetch":
			r.Header.Set("Sec-Fetch-Mode", "cors")
		case "http":
			r.TLS = nil
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 && w.Code != 400 {
			t.Fatal(kind, w.Code)
		}
	}
}

func TestOIDCRequestPinsRuntimeAndLogoutCancelsAllGenerations(t *testing.T) {
	s := oidcTestServer(t)
	runtime := sessionRuntime{backend: &fakeBackend{}, lifetime: &backendLifetime{cleanup: func() {}}}
	m := newOIDCGenerations(s.life, runtime, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	sess := &session{runtime: runtime, ctx: m.ctx, cancel: m.cancel, expires: m.deadline, csrf: "csrf", oidc: &oidcSession{generations: m, grant: &oidc.Grant{}}}
	token := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	hash := sha256.Sum256([]byte(token))
	s.sessions[hash] = sess
	r := oidcRequest("/api/session")
	r.AddCookie(&http.Cookie{Name: nativeCookieName, Value: token})
	pinned, key := s.authenticate(r)
	if pinned == nil {
		t.Fatal("missing session")
	}
	defer pinned.runtime.release()
	r = r.WithContext(context.WithValue(r.Context(), oidcRequestKey{}, oidcRequestSession{pinned, key}))
	nextRuntime := sessionRuntime{backend: &fakeBackend{}, lifetime: &backendLifetime{cleanup: func() {}}}
	if err := m.refresh(context.Background(), func(context.Context) (sessionRuntime, time.Time, error) {
		return nextRuntime, time.Now().Add(time.Minute), nil
	}); err != nil {
		t.Fatal(err)
	}
	same, _ := s.authenticate(r)
	if same != pinned || same.runtime.lifetime != runtime.lifetime {
		t.Fatal("request changed generations")
	}
	if !sameSession(same, sess) {
		t.Fatal("ownership changed on refresh")
	}
	s.mu.Lock()
	s.removeSessionLocked(hash, pinned)
	s.mu.Unlock()
	if m.ctx.Err() == nil || pinned.ctx.Err() == nil {
		t.Fatal("logout failed to cancel generations")
	}
	if _, err := m.acquire(); err == nil {
		t.Fatal("logout resurrected")
	}
}

func TestOIDCRefreshAndLogoutKeepOtherUserIsolated(t *testing.T) {
	s := oidcTestServer(t)
	add := func(token, name string) (*session, [32]byte) {
		runtime := sessionRuntime{backend: &fakeBackend{list: func(context.Context) ([]consoleapi.Bucket, error) { return []consoleapi.Bucket{{Name: name}}, nil }}, lifetime: &backendLifetime{cleanup: func() {}}}
		m := newOIDCGenerations(s.life, runtime, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
		root := &session{runtime: runtime, ctx: m.ctx, cancel: m.cancel, expires: m.deadline, csrf: "csrf", oidc: &oidcSession{generations: m, grant: &oidc.Grant{}}}
		hash := sha256.Sum256([]byte(token))
		s.sessions[hash] = root
		return root, hash
	}
	aToken, bToken := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG", "abcdefghijklmnopqrstuvwxyz0123456789HIJKLMN"
	first, firstHash := add(aToken, "first-old")
	second, _ := add(bToken, "second")
	if sameSession(first, second) {
		t.Fatal("two users shared task owner")
	}
	next := sessionRuntime{backend: &fakeBackend{list: func(context.Context) ([]consoleapi.Bucket, error) {
		return []consoleapi.Bucket{{Name: "first-new"}}, nil
	}}, lifetime: &backendLifetime{cleanup: func() {}}}
	if err := first.oidc.generations.refresh(context.Background(), func(context.Context) (sessionRuntime, time.Time, error) {
		return next, time.Now().Add(time.Minute), nil
	}); err != nil {
		t.Fatal(err)
	}
	probe := func(token, expect string) {
		r := oidcRequest("/api/buckets")
		r.AddCookie(&http.Cookie{Name: nativeCookieName, Value: token})
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), expect) {
			t.Fatal("wrong per-user connection", w.Code, w.Body.String())
		}
	}
	probe(aToken, "first-new")
	probe(bToken, "second")
	s.mu.Lock()
	s.removeSessionLocked(firstHash, first)
	s.mu.Unlock()
	if second.ctx.Err() != nil {
		t.Fatal("other user's session canceled")
	}
	probe(bToken, "second")
}

func TestOIDCPinnedRequestRejectsAbsoluteExpiryBeforeTimerRuns(t *testing.T) {
	s := oidcTestServer(t)
	// An uncanceled context models a deadline timer awaiting scheduler time.
	sess := &session{ctx: context.Background(), expires: time.Now().Add(-time.Second)}
	r := oidcRequest("/api/session")
	r = r.WithContext(context.WithValue(r.Context(), oidcRequestKey{}, oidcRequestSession{sess: sess}))
	if authenticated, _ := s.authenticate(r); authenticated != nil {
		t.Fatal("pinned request bypassed absolute credential expiry")
	}
}

func TestOIDCReturnEstablishesDocument(t *testing.T) {
	w := httptest.NewRecorder()
	writeOIDCReturn(w)
	if w.Code != http.StatusOK || w.Header().Get("Location") != "" || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("return must establish a local document: %d %v", w.Code, w.Header())
	}
	if !strings.Contains(w.Body.String(), `content="0;url=/"`) || !strings.Contains(w.Body.String(), `href="/"`) {
		t.Fatal("return document must use fixed local navigation")
	}
}

func TestOIDCReturnDoesNotExemptCrossSiteHomeOrAPI(t *testing.T) {
	s := oidcTestServer(t)
	for _, path := range []string{"/", "/api/session", "/api/oidc/login"} {
		r := oidcRequest(path)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.Header.Set("Sec-Fetch-Dest", "document")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("return must not bypass cross-site guard at %s: %d", path, w.Code)
		}
	}
}

func TestOIDCPrincipalSessionBudgetIsolated(t *testing.T) {
	s := oidcTestServer(t)
	first, err := OIDCPrincipalIdentity("storage", "https://idp.example", "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := OIDCPrincipalIdentity("storage", "https://idp.example", "second")
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < 4; i++ {
		if s.principalSessionLimitLocked(first) {
			t.Fatal("session rejected before principal budget")
		}
		s.sessions[sha256.Sum256([]byte{byte(i)})] = &session{runtime: sessionRuntime{identity: first}}
	}
	if !s.principalSessionLimitLocked(first) {
		t.Fatal("OIDC principal can exhaust global capacity")
	}
	if s.principalSessionLimitLocked(second) {
		t.Fatal("different subject shares session budget")
	}
	delete(s.sessions, sha256.Sum256([]byte{0}))
	if s.principalSessionLimitLocked(first) {
		t.Fatal("logout did not release principal capacity")
	}
	// These entries represent budget occupancy, without owned clients/workers.
	clear(s.sessions)
}

func TestOIDCInvalidCallbacksDoNotSpendLoginBudget(t *testing.T) {
	s := oidcTestServer(t)
	for i := 0; i < maxLoginAttempts+1; i++ {
		w := httptest.NewRecorder()
		s.serveOIDC(w, oidcRequest("/api/oidc/callback?state=a&code=b"))
		if w.Code != 400 {
			t.Fatalf("invalid callback consumed login capacity: %d", w.Code)
		}
	}
	s.mu.Lock()
	if s.loginCount != 0 {
		s.mu.Unlock()
		t.Fatal("callbacks exhausted login starts")
	}
	s.loginCount = maxLoginAttempts
	s.loginWindow = time.Now()
	s.mu.Unlock()
	w := httptest.NewRecorder()
	s.serveOIDC(w, oidcRequest("/api/oidc/callback?state=a&code=b"))
	if w.Code != 400 {
		t.Fatal("callback charged an already started attempt twice")
	}
	w = httptest.NewRecorder()
	s.serveOIDC(w, oidcRequest("/api/oidc/login"))
	if w.Code != 429 {
		t.Fatal("login-start rate limit removed")
	}
}
