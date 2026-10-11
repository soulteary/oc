package oidc

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/url"
	"sync"
	"time"
)

// Credentials remain in-process; JSON serialization is explicitly rejected.
// The caller constructs an owned S3 client including SessionToken and must stop
// all requests at Expires. Expiry does not imply upstream revocation of keys.
type Credentials struct {
	AccessKey, SecretKey, SessionToken string
	Expires                            time.Time
}

func (Credentials) String() string               { return "oidc.Credentials{redacted}" }
func (Credentials) GoString() string             { return "oidc.Credentials{redacted}" }
func (Credentials) MarshalJSON() ([]byte, error) { return nil, ErrConfiguration }

// Grant is an opaque, single-client identity and serializes refresh exchanges.
// Close drops references; Go strings cannot be securely erased from memory.
type Grant struct{ *grantState }

type grantState struct {
	refreshGate                     chan struct{}
	cancel                          context.CancelFunc
	mu                              sync.Mutex
	client                          *Client
	issuer, subject, nonce, refresh string
	issued                          int64
	credentials                     Credentials
	deadline                        time.Time
	closed                          bool
}

func (Grant) String() string               { return "oidc.Grant{redacted}" }
func (Grant) GoString() string             { return "oidc.Grant{redacted}" }
func (Grant) MarshalJSON() ([]byte, error) { return nil, ErrConfiguration }
func (g *Grant) Identity() (issuer, subject string) {
	if g == nil || g.grantState == nil {
		return "", ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.issuer, g.subject
}

// SessionDeadline is the immutable lifetime bound established by Complete.
func (g *Grant) SessionDeadline() time.Time {
	if g == nil || g.grantState == nil {
		return time.Time{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.deadline
}
func (g *Grant) Credentials() (Credentials, error) {
	if g == nil || g.grantState == nil {
		return Credentials{}, ErrLogin
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || !g.credentials.Expires.After(g.client.now()) {
		return Credentials{}, ErrLogin
	}
	return g.credentials, nil
}
func (g *Grant) Close() {
	if g == nil || g.grantState == nil {
		return
	}
	g.mu.Lock()
	g.closed = true
	g.refresh = ""
	g.credentials = Credentials{}
	cancel := g.cancel
	g.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Refresh serializes exchanges without holding the state lock over network I/O.
// Close marks closed and cancels an in-flight exchange immediately. A late
// response can never commit replacement credentials to a closed grant.
func (g *Grant) Refresh(ctx context.Context) (Credentials, error) {
	if g == nil || g.grantState == nil {
		return Credentials{}, ErrRefresh
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case g.refreshGate <- struct{}{}:
		defer func() { <-g.refreshGate }()
	case <-ctx.Done():
		return Credentials{}, ErrRefresh
	}
	// select may choose an available gate even when cancellation is ready.
	// Until state is consumed, this caller owns no exchange to invalidate.
	if ctx.Err() != nil {
		return Credentials{}, ErrRefresh
	}
	g.mu.Lock()
	if ctx.Err() != nil {
		g.mu.Unlock()
		return Credentials{}, ErrRefresh
	}
	if g.closed || g.refresh == "" || !g.credentials.Expires.After(g.client.now()) {
		g.closed = true
		g.refresh = ""
		g.credentials = Credentials{}
		g.mu.Unlock()
		return Credentials{}, ErrRefresh
	}
	old := g.refresh
	g.refresh = ""
	g.cancel = cancel
	issuer, subject, nonce, issued, deadline := g.issuer, g.subject, g.nonce, g.issued, g.deadline
	oldExpires := g.credentials.Expires
	g.mu.Unlock()
	exchangeCtx, exchangeCancel := context.WithDeadline(ctx, oldExpires)
	defer exchangeCancel()
	tr, err := g.client.token(exchangeCtx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {old}})
	var cl claims
	var creds Credentials
	if err == nil {
		cl, err = g.client.verify(exchangeCtx, tr.IDToken, nonce, true)
	}
	if err == nil && (cl.Issuer != issuer || cl.Subject != subject || cl.Issued < issued) {
		err = ErrLogin
	}
	if err == nil {
		creds, err = g.client.assume(exchangeCtx, tr.IDToken, cl)
	}
	if err == nil {
		if creds.Expires.After(deadline) {
			creds.Expires = deadline
		}
		if !creds.Expires.After(g.client.now()) {
			err = ErrLogin
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cancel = nil
	if err != nil || g.closed || exchangeCtx.Err() != nil || !oldExpires.After(g.client.now()) {
		g.closed = true
		g.refresh = ""
		g.credentials = Credentials{}
		return Credentials{}, ErrRefresh
	}
	g.issued = cl.Issued
	g.credentials = creds
	g.refresh = tr.RefreshToken
	if g.refresh == "" {
		g.refresh = old
	}
	return creds, nil
}
func (c *Client) assume(ctx context.Context, token string, cl claims) (Credentials, error) {
	form := url.Values{"Action": {"AssumeRoleWithWebIdentity"}, "Version": {"2011-06-15"}, "WebIdentityToken": {token}}
	if c.config.SessionPolicy != "" {
		form.Set("Policy", c.config.SessionPolicy)
	}
	// No DurationSeconds: pinned OtterIO retains ID token expiry as-is.
	b, err := c.request(ctx, "POST", c.config.STSEndpoint, form)
	if err != nil {
		return Credentials{}, err
	}
	var res struct {
		XMLName xml.Name `xml:"https://sts.amazonaws.com/doc/2011-06-15/ AssumeRoleWithWebIdentityResponse"`
		Result  struct {
			Subject     string `xml:"SubjectFromWebIdentityToken"`
			Audience    string `xml:"Audience"`
			Provider    string `xml:"Provider"`
			Credentials struct {
				AccessKey    string    `xml:"AccessKeyId"`
				SecretKey    string    `xml:"SecretAccessKey"`
				SessionToken string    `xml:"SessionToken"`
				Expiration   time.Time `xml:"Expiration"`
			} `xml:"Credentials"`
		} `xml:"AssumeRoleWithWebIdentityResult"`
	}
	if xml.Unmarshal(b, &res) != nil || res.Result.Subject != cl.Subject || (res.Result.Audience != "" && res.Result.Audience != c.config.ClientID) || (res.Result.Provider != "" && res.Result.Provider != cl.Issuer) {
		return Credentials{}, ErrProtocol
	}
	v := res.Result.Credentials
	if v.AccessKey == "" || v.SecretKey == "" || v.SessionToken == "" || !v.Expiration.After(c.now()) {
		return Credentials{}, ErrProtocol
	}
	expires := time.Unix(cl.Expires, 0)
	if v.Expiration.Before(expires) {
		expires = v.Expiration
	}
	if !expires.After(c.now()) {
		return Credentials{}, ErrLogin
	}
	return Credentials{v.AccessKey, v.SecretKey, v.SessionToken, expires}, nil
}

// Assert compile-time serialization behavior without exposing secret fields.
var _ json.Marshaler = Credentials{}
