package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixture struct {
	omitID                         bool
	delay                          time.Duration
	server                         *httptest.Server
	key                            *rsa.PrivateKey
	mu                             sync.Mutex
	nonce, token, refresh, subject string
	form                           url.Values
	calls                          int
	mutate                         func(map[string]any)
	redirect, failRefresh          bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{key: k, refresh: "refresh-one", subject: "alice"}
	f.server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}
func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		if f.redirect {
			http.Redirect(w, r, "https://untrusted.invalid/discovery", http.StatusFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize", "token_endpoint": f.server.URL + "/token", "jwks_uri": f.server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic"}})
	case "/keys":
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "current", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes())}}})
	case "/token":
		if f.delay > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(f.delay):
			}
		}
		_ = r.ParseForm()
		f.form = r.PostForm
		f.calls++
		if f.failRefresh && r.Form.Get("grant_type") == "refresh_token" {
			http.Error(w, "invalid_grant secret-reflected", 400)
			return
		}
		cl := map[string]any{"iss": f.server.URL, "aud": "console", "sub": f.subject, "exp": time.Now().Add(2 * time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": f.nonce}
		if f.mutate != nil {
			f.mutate(cl)
		}
		f.token = sign(f.key, cl)
		if f.omitID {
			f.token = ""
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id_token": f.token, "token_type": "Bearer", "refresh_token": f.refresh})
	case "/sts":
		_ = r.ParseForm()
		if r.Form.Get("Action") != "AssumeRoleWithWebIdentity" || r.Form.Get("WebIdentityToken") != f.token || r.Form.Get("Version") != "2011-06-15" || r.Header.Get("Authorization") != "" {
			http.Error(w, "invalid", 400)
			return
		}
		expiry := time.Now().Add(90 * time.Minute).UTC().Format(time.RFC3339)
		_, _ = fmt.Fprintf(w, `<AssumeRoleWithWebIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><AssumeRoleWithWebIdentityResult><SubjectFromWebIdentityToken>%s</SubjectFromWebIdentityToken><Credentials><AccessKeyId>temporary-key</AccessKeyId><SecretAccessKey>temporary-secret</SecretAccessKey><SessionToken>session-secret</SessionToken><Expiration>%s</Expiration></Credentials></AssumeRoleWithWebIdentityResult></AssumeRoleWithWebIdentityResponse>`, f.subject, expiry)
	default:
		http.NotFound(w, r)
	}
}
func sign(k *rsa.PrivateKey, cl map[string]any) string {
	h, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "current"})
	b, _ := json.Marshal(cl)
	message := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(message))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, sum[:])
	return message + "." + base64.RawURLEncoding.EncodeToString(sig)
}
func (f *fixture) client(t *testing.T) *Client {
	t.Helper()
	c, err := New(context.Background(), Config{Issuer: f.server.URL, ClientID: "console", RedirectURI: "https://console.example/callback", STSEndpoint: f.server.URL + "/sts", Transport: f.server.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const binding = "independent-unguessable-browser-cookie-32-bytes"

func (f *fixture) begin(t *testing.T, c *Client) Authorization {
	t.Helper()
	a, err := c.Begin(binding)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(a.URL)
	f.mu.Lock()
	f.nonce = u.Query().Get("nonce")
	f.mu.Unlock()
	return a
}
func TestFlowRefreshAndRedaction(t *testing.T) {
	f := newFixture(t)
	c := f.client(t)
	a := f.begin(t, c)
	g, err := c.Complete(context.Background(), binding, a.State, "authorization-code")
	if err != nil {
		t.Fatal(err)
	}
	issuer, sub := g.Identity()
	if issuer != f.server.URL || sub != "alice" {
		t.Fatal("unverified identity")
	}
	creds, err := g.Credentials()
	if err != nil || creds.SessionToken != "session-secret" || creds.Expires.After(time.Now().Add(time.Hour)) {
		t.Fatal("bad credentials/expiry", err)
	}
	f.mu.Lock()
	verifier := f.form.Get("code_verifier")
	if f.form.Get("code") != "authorization-code" || f.form.Get("redirect_uri") != "https://console.example/callback" {
		t.Fatal("bad exchange")
	}
	f.mu.Unlock()
	u, _ := url.Parse(a.URL)
	digest := sha256.Sum256([]byte(verifier))
	if u.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(digest[:]) {
		t.Fatal("PKCE mismatch")
	}
	if _, err := c.Complete(context.Background(), binding, a.State, "authorization-code"); err != ErrLogin {
		t.Fatal("state replay", err)
	}
	f.mu.Lock()
	f.refresh = "refresh-two"
	f.mu.Unlock()
	if _, err := g.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.form.Get("refresh_token") != "refresh-one" {
		t.Fatal("old refresh not used")
	}
	f.mu.Unlock()
	if _, err := g.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.form.Get("refresh_token") != "refresh-two" {
		t.Fatal("rotation ignored")
	}
	f.mu.Unlock()
	for _, v := range []any{g, creds, c.config} {
		if _, err := json.Marshal(v); err == nil {
			t.Fatal("secret serializable")
		}
		if strings.Contains(fmt.Sprintf("%#v %v", v, v), "secret") {
			t.Fatal("secret logged")
		}
	}
	g.Close()
	if _, err := g.Credentials(); err != ErrLogin {
		t.Fatal("closed grant")
	}
}
func TestClaimsRejected(t *testing.T) {
	cases := map[string]func(map[string]any){"issuer": func(m map[string]any) { m["iss"] = "https://other.example" }, "audience": func(m map[string]any) { m["aud"] = "other" }, "azp": func(m map[string]any) { m["aud"] = []string{"console", "other"} }, "nonce": func(m map[string]any) { m["nonce"] = "wrong" }, "expired": func(m map[string]any) { m["exp"] = time.Now().Add(-time.Minute).Unix() }, "future": func(m map[string]any) { m["iat"] = time.Now().Add(time.Hour).Unix() }, "nbf": func(m map[string]any) { m["nbf"] = time.Now().Add(time.Hour).Unix() }, "subject": func(m map[string]any) { m["sub"] = "" }}
	f := newFixture(t)
	c := f.client(t)
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f.mu.Lock()
			f.mutate = mutate
			f.mu.Unlock()
			a := f.begin(t, c)
			if _, err := c.Complete(context.Background(), binding, a.State, "code"); err != ErrLogin {
				t.Fatal("accepted claims", err)
			}
		})
	}
}
func TestBindingExpiryCancellationAndRefreshFailure(t *testing.T) {
	f := newFixture(t)
	c := f.client(t)
	a := f.begin(t, c)
	if _, err := c.Complete(context.Background(), "wrong-binding", a.State, "code"); err != ErrLogin {
		t.Fatal(err)
	}
	g, err := c.Complete(context.Background(), binding, a.State, "code")
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.failRefresh = true
	f.mu.Unlock()
	if _, err := g.Refresh(context.Background()); err != ErrRefresh {
		t.Fatal(err)
	}
	if _, err := g.Credentials(); err != ErrLogin {
		t.Fatal("failed refresh retained grant")
	}
	before := f.calls
	if _, err := g.Refresh(context.Background()); err != ErrRefresh || f.calls != before {
		t.Fatal("refresh replay")
	}
	a = f.begin(t, c)
	c.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	if _, err := c.Complete(context.Background(), binding, a.State, "code"); err != ErrLogin {
		t.Fatal("expired state")
	}
	c.now = time.Now
	a = f.begin(t, c)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Complete(ctx, binding, a.State, "code"); err != ErrUnavailable {
		t.Fatal("canceled exchange", err)
	}
	if _, err := c.Complete(context.Background(), binding, a.State, "code"); err != ErrLogin {
		t.Fatal("canceled state replay")
	}
}
func TestTrustAndKeyRotation(t *testing.T) {
	f := newFixture(t)
	if _, err := New(context.Background(), Config{Issuer: "http://localhost", ClientID: "console"}); err != ErrConfiguration {
		t.Fatal("HTTP accepted")
	}
	f.redirect = true
	if _, err := New(context.Background(), Config{Issuer: f.server.URL, ClientID: "console", RedirectURI: "https://console.example/callback", STSEndpoint: f.server.URL + "/sts", Transport: f.server.Client().Transport}); err != ErrProtocol {
		t.Fatal("redirect accepted", err)
	}
	f.redirect = false
	c := f.client(t)
	a := f.begin(t, c)
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	f.mu.Lock()
	f.key = k
	f.mu.Unlock()
	// A signature mismatch with an existing kid is rejected; key refresh happens
	// at cache expiry, preventing attacker-triggered repeated JWKS traffic.
	if _, err := c.Complete(context.Background(), binding, a.State, "code"); err != ErrLogin {
		t.Fatal("invalid signature accepted")
	}
	c.mu.Lock()
	c.keysExpires = time.Now().Add(-time.Second)
	c.lastKeyRefresh = time.Now().Add(-time.Minute)
	c.mu.Unlock()
	a = f.begin(t, c)
	if _, err := c.Complete(context.Background(), binding, a.State, "code"); err != nil {
		t.Fatal("rotation not loaded", err)
	}
}

func TestRefreshRequiresVerifiedRenewedIdentity(t *testing.T) {
	cases := []string{"unsupported", "no-id-token", "different-subject", "expired-id-token"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if name == "unsupported" {
				f.refresh = ""
			}
			c := f.client(t)
			a := f.begin(t, c)
			g, err := c.Complete(context.Background(), binding, a.State, "code")
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			switch name {
			case "no-id-token":
				f.omitID = true
			case "different-subject":
				f.mutate = func(m map[string]any) { m["sub"] = "bob" }
			case "expired-id-token":
				f.mutate = func(m map[string]any) { m["exp"] = time.Now().Add(-time.Minute).Unix() }
			}
			f.mu.Unlock()
			if _, err := g.Refresh(context.Background()); err != ErrRefresh {
				t.Fatal("unsafe refresh", err)
			}
			if _, err := g.Credentials(); err != ErrLogin {
				t.Fatal("invalid refresh still active")
			}
		})
	}
}
func TestTimeoutAndConcurrentRefresh(t *testing.T) {
	f := newFixture(t)
	c := f.client(t)
	a := f.begin(t, c)
	g, err := c.Complete(context.Background(), binding, a.State, "code")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.Refresh(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	g.Close()
	f.mu.Lock()
	f.delay = 100 * time.Millisecond
	f.mu.Unlock()
	c.http.Timeout = 20 * time.Millisecond
	a = f.begin(t, c)
	started := time.Now()
	if _, err := c.Complete(context.Background(), binding, a.State, "slow-code"); err != ErrUnavailable {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("timeout did not bound request")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCloseCancelsRefreshWithoutWaiting(t *testing.T) {
	f := newFixture(t)
	c := f.client(t)
	a := f.begin(t, c)
	g, err := c.Complete(context.Background(), binding, a.State, "code")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	base := c.http.Transport
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/token" {
			close(entered)
			<-release
		}
		return base.RoundTrip(r)
	})
	result := make(chan error, 1)
	go func() { _, err := g.Refresh(context.Background()); result <- err }()
	<-entered
	closed := make(chan struct{})
	go func() { g.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("Close waited for upstream refresh")
	}
	if _, err := g.Credentials(); err != ErrLogin {
		t.Fatal("Close retained credentials")
	}
	// A queued refresh can cancel immediately while the first upstream ignores ctx.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Refresh(ctx); err != ErrRefresh {
		t.Fatal("queued cancel failed")
	}
	close(release)
	if err := <-result; err != ErrRefresh {
		t.Fatal("late refresh committed", err)
	}
}
func TestExpiredJWKSConcurrentVerificationSharesFetch(t *testing.T) {
	f := newFixture(t)
	c := f.client(t)
	a := f.begin(t, c)
	_ = a
	token := sign(f.key, map[string]any{"iss": f.server.URL, "aud": "console", "sub": "alice", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": f.nonce})
	c.mu.Lock()
	c.keysExpires = time.Now().Add(-time.Second)
	c.lastKeyRefresh = time.Now().Add(-time.Minute)
	c.mu.Unlock()
	base := c.http.Transport
	entered := make(chan struct{})
	release := make(chan struct{})
	var count atomic.Int32
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/keys" {
			if count.Add(1) == 1 {
				close(entered)
			}
			<-release
		}
		return base.RoundTrip(r)
	})
	result := make(chan error, 12)
	start := make(chan struct{})
	for range 12 {
		go func() { <-start; _, err := c.verify(context.Background(), token, f.nonce, false); result <- err }()
	}
	close(start)
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.verify(ctx, token, f.nonce, false); err != ErrUnavailable {
		t.Fatal("JWKS waiter cancellation", err)
	}
	close(release)
	for range 12 {
		if err := <-result; err != nil {
			t.Fatal("valid token rejected while cache refresh in flight", err)
		}
	}
	if count.Load() != 1 {
		t.Fatal("duplicate fetch", count.Load())
	}
}
func TestUpstreamReturnCannotCommitExpiredCredentials(t *testing.T) {
	for _, name := range []string{"id-token-expires-during-sts", "session-deadline-during-refresh", "old-credentials-expire-during-refresh"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			c := f.client(t)
			baseTime := time.Now()
			var clock atomic.Int64
			clock.Store(baseTime.UnixNano())
			c.now = func() time.Time { return time.Unix(0, clock.Load()) }
			var g *Grant
			if name != "id-token-expires-during-sts" {
				a := f.begin(t, c)
				var err error
				g, err = c.Complete(context.Background(), binding, a.State, "code")
				if err != nil {
					t.Fatal(err)
				}
				if name == "session-deadline-during-refresh" {
					g.deadline = baseTime.Add(5 * time.Second)
				} else {
					g.credentials.Expires = baseTime.Add(5 * time.Second)
				}
			} else {
				f.mutate = func(m map[string]any) { m["exp"] = baseTime.Add(5 * time.Second).Unix() }
			}
			base := c.http.Transport
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				res, err := base.RoundTrip(r)
				if r.URL.Path == "/sts" {
					clock.Add(int64(10 * time.Second))
				}
				return res, err
			})
			if g != nil {
				if _, err := g.Refresh(context.Background()); err != ErrRefresh {
					t.Fatal("expired refreshed session committed", err)
				}
				if _, err := g.Credentials(); err != ErrLogin {
					t.Fatal("expired grant not closed")
				}
			} else {
				a := f.begin(t, c)
				if _, err := c.Complete(context.Background(), binding, a.State, "code"); err != ErrLogin {
					t.Fatal("expired login committed", err)
				}
			}
		})
	}
}

func TestSlowJWKSFailureSharedWithoutRefetch(t *testing.T) {
	f := newFixture(t)
	c := f.client(t)
	baseTime := time.Now()
	var clock atomic.Int64
	clock.Store(baseTime.UnixNano())
	c.now = func() time.Time { return time.Unix(0, clock.Load()) }
	c.mu.Lock()
	c.keysExpires = baseTime.Add(-time.Second)
	c.lastKeyRefresh = baseTime.Add(-time.Minute)
	c.mu.Unlock()
	entered := make(chan struct{})
	release := make(chan struct{})
	var count atomic.Int32
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if count.Add(1) == 1 {
			close(entered)
		}
		<-release
		clock.Add(int64(2 * time.Second))
		return nil, errors.New("private upstream failure")
	})
	start := make(chan struct{})
	result := make(chan error, 12)
	for range 12 {
		go func() { <-start; _, err := c.verificationKey(context.Background(), "current"); result <- err }()
	}
	close(start)
	<-entered
	close(release)
	for range 12 {
		if err := <-result; err != ErrUnavailable {
			t.Fatal("failed fetch not shared", err)
		}
	}
	if count.Load() != 1 {
		t.Fatal("waiters retried slow failing fetch", count.Load())
	}
}

func TestQueuedRefreshCancellationPreservesActiveExchange(t *testing.T) {
	f := newFixture(t)
	c := f.client(t)
	a := f.begin(t, c)
	g, err := c.Complete(context.Background(), binding, a.State, "code")
	if err != nil {
		t.Fatal(err)
	}
	before, err := g.Credentials()
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	base := c.http.Transport
	var tokenCalls atomic.Int32
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/token" {
			tokenCalls.Add(1)
			close(entered)
			<-release
			if r.Context().Err() != nil {
				t.Error("queued cancellation canceled active request")
			}
		}
		return base.RoundTrip(r)
	})
	active := make(chan error, 1)
	go func() { _, err := g.Refresh(context.Background()); active <- err }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() { _, err := g.Refresh(ctx); queued <- err }()
	cancel()
	if err := <-queued; err != ErrRefresh {
		t.Fatal("queued refresh result", err)
	}
	during, err := g.Credentials()
	if err != nil || during != before {
		close(release)
		t.Fatal("queued cancellation modified valid credentials", err)
	}
	if tokenCalls.Load() != 1 {
		close(release)
		t.Fatal("canceled queued caller started a request", tokenCalls.Load())
	}
	close(release)
	if err := <-active; err != nil {
		t.Fatal("active refresh failed after queued cancellation", err)
	}
	if _, err := g.Credentials(); err != nil {
		t.Fatal("active grant was closed", err)
	}
	if tokenCalls.Load() != 1 {
		t.Fatal("queued request escaped cancellation")
	}
}

func TestCanceledRefreshWithAvailableGateDoesNotConsumeState(t *testing.T) {
	f := newFixture(t)
	c := f.client(t)
	a := f.begin(t, c)
	g, err := c.Complete(context.Background(), binding, a.State, "code")
	if err != nil {
		t.Fatal(err)
	}
	before, err := g.Credentials()
	if err != nil {
		t.Fatal(err)
	}
	base := c.http.Transport
	var calls atomic.Int32
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { calls.Add(1); return base.RoundTrip(r) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Both select arms are ready. Every possible selection must preserve state.
	for range 32 {
		if _, err := g.Refresh(ctx); err != ErrRefresh {
			t.Fatal(err)
		}
	}
	after, err := g.Credentials()
	if err != nil || before != after {
		t.Fatal("pre-exchange cancellation changed grant", err)
	}
	if calls.Load() != 0 {
		t.Fatal("canceled caller reached upstream", calls.Load())
	}
	if _, err := g.Refresh(context.Background()); err != nil {
		t.Fatal("refresh token consumed by canceled caller", err)
	}
}

func TestOpaqueHandleFormattingRedactsPointerAndValueSecrets(t *testing.T) {
	f := newFixture(t)
	c := f.client(t)
	a := f.begin(t, c)
	g, err := c.Complete(context.Background(), binding, a.State, "code")
	if err != nil {
		t.Fatal(err)
	}
	sentinels := []string{"CLIENT-SECRET-PRIVATE-SENTINEL", "REFRESH-TOKEN-PRIVATE-SENTINEL", "STS-SECRET-PRIVATE-SENTINEL", "STS-SESSION-PRIVATE-SENTINEL"}
	c.config.ClientSecret = sentinels[0]
	g.refresh = sentinels[1]
	g.credentials.SecretKey = sentinels[2]
	g.credentials.SessionToken = sentinels[3]
	handles := []struct {
		name  string
		value any
	}{
		{"client pointer", c}, {"client value", *c}, {"grant pointer", g}, {"grant value", *g},
	}
	for _, handle := range handles {
		t.Run(handle.name, func(t *testing.T) {
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				output := fmt.Sprintf(format, handle.value)
				if !strings.Contains(output, "redacted") {
					t.Errorf("format %s did not use redaction", format)
				}
				for _, secret := range sentinels {
					if strings.Contains(output, secret) {
						t.Errorf("format %s exposed a secret", format)
					}
				}
			}
			if _, err := json.Marshal(handle.value); err == nil {
				t.Error("handle permitted JSON export")
			}
		})
	}
}

func TestOpaqueHandleZeroAndNilSafety(t *testing.T) {
	var c *Client
	var g *Grant
	for _, value := range []any{c, g, Client{}, Grant{}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			output := fmt.Sprintf(format, value)
			if strings.Contains(output, "PANIC") {
				t.Fatalf("nil/zero formatting panicked: %s", format)
			}
		}
	}
	for _, client := range []*Client{nil, {}} {
		if _, err := client.Begin(binding); err != ErrConfiguration {
			t.Fatal("nil/zero client accepted Begin")
		}
		if _, err := client.Complete(context.Background(), binding, "state", "code"); err != ErrConfiguration {
			t.Fatal("nil/zero client accepted Complete")
		}
	}
	for _, grant := range []*Grant{nil, {}} {
		issuer, subject := grant.Identity()
		if issuer != "" || subject != "" {
			t.Fatal("nil/zero identity not empty")
		}
		if _, err := grant.Credentials(); err != ErrLogin {
			t.Fatal("nil/zero grant accepted credentials")
		}
		if _, err := grant.Refresh(context.Background()); err != ErrRefresh {
			t.Fatal("nil/zero grant accepted refresh")
		}
		grant.Close()
	}
}

func TestOpaqueHandleCopiesShareLifecycleWithoutCopyingLocks(t *testing.T) {
	f := newFixture(t)
	c := f.client(t)
	clientCopy := *c
	a := f.begin(t, &clientCopy)
	g, err := c.Complete(context.Background(), binding, a.State, "code")
	if err != nil {
		t.Fatal("client copy lost shared pending state", err)
	}
	grantCopy := *g
	grantCopy.Close()
	if _, err := g.Credentials(); err != ErrLogin {
		t.Fatal("grant copy did not share closed state")
	}
}
