// Package oidc implements a deliberately narrow authorization-code / RS256
// profile. It does not install console routes or establish console sessions.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrConfiguration = errors.New("oidc: invalid trusted configuration")
	ErrProtocol      = errors.New("oidc: upstream protocol rejected")
	ErrLogin         = errors.New("oidc: login rejected; start a new login")
	ErrUnavailable   = errors.New("oidc: upstream unavailable")
	ErrRefresh       = errors.New("oidc: refresh unavailable; start a new login")
	ErrBusy          = errors.New("oidc: login capacity reached")
)

// Config is supplied by the administrator, never from a browser request. All
// URLs use HTTPS. Discovery endpoints must remain on the issuer origin.
type Config struct {
	Issuer, ClientID, ClientSecret, RedirectURI, STSEndpoint string
	Scopes                                                   []string
	SessionPolicy                                            string
	// Transport may add a deployment's trusted CA, but must retain TLS verification.
	Transport          http.RoundTripper
	Timeout            time.Duration
	MaxSessionLifetime time.Duration
}

func (Config) MarshalJSON() ([]byte, error) { return nil, ErrConfiguration }
func (Config) String() string               { return "oidc.Config{redacted}" }
func (Config) GoString() string             { return "oidc.Config{redacted}" }

type discovery struct {
	Issuer        string   `json:"issuer"`
	Authorization string   `json:"authorization_endpoint"`
	Token         string   `json:"token_endpoint"`
	JWKS          string   `json:"jwks_uri"`
	Algorithms    []string `json:"id_token_signing_alg_values_supported"`
	ResponseTypes []string `json:"response_types_supported"`
	PKCE          []string `json:"code_challenge_methods_supported"`
	AuthMethods   []string `json:"token_endpoint_auth_methods_supported"`
}
type pending struct {
	binding         [32]byte
	nonce, verifier string
	expires         time.Time
}

// Client is an opaque handle. Copies share state and never copy its locks.
type Client struct{ *clientState }

type clientState struct {
	config         Config
	http           *http.Client
	discovery      discovery
	mu             sync.Mutex
	pending        map[string]pending
	keys           map[string]key
	keysExpires    time.Time
	lastKeyRefresh time.Time
	keyFlight      chan struct{}
	keyError       error
	now            func() time.Time
}

func (Client) String() string               { return "oidc.Client{redacted}" }
func (Client) GoString() string             { return "oidc.Client{redacted}" }
func (Client) MarshalJSON() ([]byte, error) { return nil, ErrConfiguration }

// Authorization's state is consumed once and expires after five minutes. The
// caller must bind it to a random, HttpOnly browser cookie, independent of state.
type Authorization struct {
	URL, State string
	Expires    time.Time
}

func (Authorization) String() string   { return "oidc.Authorization{redacted}" }
func (Authorization) GoString() string { return "oidc.Authorization{redacted}" }

func validURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	return u, err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == ""
}
func New(ctx context.Context, cfg Config) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	issuer, ok := validURL(cfg.Issuer)
	if !ok {
		return nil, ErrConfiguration
	}
	if _, ok = validURL(cfg.RedirectURI); !ok {
		return nil, ErrConfiguration
	}
	if _, ok = validURL(cfg.STSEndpoint); !ok {
		return nil, ErrConfiguration
	}
	if cfg.ClientID == "" || len(cfg.SessionPolicy) > 2048 || (cfg.SessionPolicy != "" && !json.Valid([]byte(cfg.SessionPolicy))) {
		return nil, ErrConfiguration
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Timeout < 0 || cfg.Timeout > 30*time.Second {
		return nil, ErrConfiguration
	}
	if cfg.MaxSessionLifetime == 0 {
		cfg.MaxSessionLifetime = time.Hour
	}
	if cfg.MaxSessionLifetime < time.Minute || cfg.MaxSessionLifetime > 24*time.Hour {
		return nil, ErrConfiguration
	}
	if tr, ok := cfg.Transport.(*http.Transport); ok && tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		return nil, ErrConfiguration
	}
	cfg.Scopes = append([]string(nil), cfg.Scopes...)
	for _, scope := range cfg.Scopes {
		if scope == "" || strings.ContainsAny(scope, " \t\r\n") {
			return nil, ErrConfiguration
		}
	}
	c := &Client{clientState: &clientState{config: cfg, http: &http.Client{Transport: cfg.Transport, Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, pending: map[string]pending{}, now: time.Now}}
	b, err := c.request(ctx, http.MethodGet, strings.TrimRight(cfg.Issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &c.discovery) != nil || c.discovery.Issuer != cfg.Issuer || !contains(c.discovery.Algorithms, "RS256") || !contains(c.discovery.ResponseTypes, "code") || !contains(c.discovery.PKCE, "S256") {
		return nil, ErrProtocol
	}
	for _, endpoint := range []string{c.discovery.Authorization, c.discovery.Token, c.discovery.JWKS} {
		u, good := validURL(endpoint)
		if !good || u.Host != issuer.Host {
			return nil, ErrProtocol
		}
	}
	method := "none"
	if cfg.ClientSecret != "" {
		method = "client_secret_basic"
	}
	// OIDC defaults absent metadata to client_secret_basic.
	if !contains(c.discovery.AuthMethods, method) && !(len(c.discovery.AuthMethods) == 0 && method == "client_secret_basic") {
		return nil, ErrProtocol
	}
	if err := c.loadKeys(ctx); err != nil {
		return nil, err
	}
	return c, nil
}
func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
func random() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", ErrUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
func (c *Client) Begin(binding string) (Authorization, error) {
	if c == nil || c.clientState == nil {
		return Authorization{}, ErrConfiguration
	}
	if len(binding) < 32 || len(binding) > 256 {
		return Authorization{}, ErrLogin
	}
	state, err := random()
	if err != nil {
		return Authorization{}, err
	}
	nonce, err := random()
	if err != nil {
		return Authorization{}, err
	}
	verifier, err := random()
	if err != nil {
		return Authorization{}, err
	}
	now := c.now()
	expires := now.Add(5 * time.Minute)
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.pending {
		if !v.expires.After(now) {
			delete(c.pending, k)
		}
	}
	if len(c.pending) >= 1024 {
		return Authorization{}, ErrBusy
	}
	c.pending[state] = pending{sha256.Sum256([]byte(binding)), nonce, verifier, expires}
	u, _ := url.Parse(c.discovery.Authorization)
	q := u.Query()
	q.Set("client_id", c.config.ClientID)
	q.Set("redirect_uri", c.config.RedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(append([]string{"openid"}, c.config.Scopes...), " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge_method", "S256")
	sum := sha256.Sum256([]byte(verifier))
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	u.RawQuery = q.Encode()
	return Authorization{u.String(), state, expires}, nil
}

// Complete accepts only the callback's code/state, never browser-supplied
// issuer, token, subject or storage target. Even failed exchanges consume state.
func (c *Client) Complete(ctx context.Context, binding, state, code string) (*Grant, error) {
	if c == nil || c.clientState == nil {
		return nil, ErrConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c.mu.Lock()
	p, ok := c.pending[state]
	candidate := sha256.Sum256([]byte(binding))
	if ok && subtle.ConstantTimeCompare(p.binding[:], candidate[:]) == 1 {
		delete(c.pending, state)
	} else {
		ok = false
	}
	c.mu.Unlock()
	if !ok || !p.expires.After(c.now()) || code == "" || len(code) > 8192 {
		return nil, ErrLogin
	}
	tr, err := c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {c.config.RedirectURI}, "code_verifier": {p.verifier}})
	if err != nil {
		return nil, err
	}
	claims, err := c.verify(ctx, tr.IDToken, p.nonce, false)
	if err != nil {
		return nil, err
	}
	creds, err := c.assume(ctx, tr.IDToken, claims)
	if err != nil {
		return nil, err
	}
	deadline := c.now().Add(c.config.MaxSessionLifetime)
	if creds.Expires.After(deadline) {
		creds.Expires = deadline
	}
	if !creds.Expires.After(c.now()) {
		return nil, ErrLogin
	}
	return &Grant{grantState: &grantState{refreshGate: make(chan struct{}, 1), deadline: deadline, client: c, issuer: claims.Issuer, subject: claims.Subject, nonce: p.nonce, issued: claims.Issued, refresh: tr.RefreshToken, credentials: creds}}, nil
}

type tokenResponse struct {
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

func (c *Client) token(ctx context.Context, form url.Values) (tokenResponse, error) {
	form.Set("client_id", c.config.ClientID)
	b, err := c.request(ctx, http.MethodPost, c.discovery.Token, form)
	if err != nil {
		return tokenResponse{}, err
	}
	var tr tokenResponse
	if json.Unmarshal(b, &tr) != nil || tr.IDToken == "" || !strings.EqualFold(tr.TokenType, "Bearer") {
		return tr, ErrProtocol
	}
	return tr, nil
}
func (c *Client) request(ctx context.Context, method, target string, form url.Values) ([]byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, ErrConfiguration
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if target == c.discovery.Token && c.config.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(c.config.ClientID), url.QueryEscape(c.config.ClientSecret))
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, ErrProtocol
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(b) > 1<<20 {
		return nil, ErrProtocol
	}
	return b, nil
}
