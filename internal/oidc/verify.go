package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"time"
)

type key = *rsa.PublicKey
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}
type claims struct {
	Issuer          string          `json:"iss"`
	Subject         string          `json:"sub"`
	Audience        json.RawMessage `json:"aud"`
	AuthorizedParty string          `json:"azp"`
	Nonce           string          `json:"nonce"`
	Expires         int64           `json:"exp"`
	Issued          int64           `json:"iat"`
	NotBefore       int64           `json:"nbf"`
}

func (c *Client) loadKeys(ctx context.Context) error {
	b, err := c.request(ctx, "GET", c.discovery.JWKS, nil)
	if err != nil {
		return err
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if json.Unmarshal(b, &doc) != nil || len(doc.Keys) > 100 {
		return ErrProtocol
	}
	keys := map[string]key{}
	for _, j := range doc.Keys {
		if j.Kty != "RSA" || (j.Use != "" && j.Use != "sig") || (j.Alg != "" && j.Alg != "RS256") {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(j.N)
		if err != nil {
			return ErrProtocol
		}
		e, err := base64.RawURLEncoding.DecodeString(j.E)
		if err != nil || len(e) == 0 || len(e) > 4 {
			return ErrProtocol
		}
		exponent := 0
		for _, v := range e {
			exponent = exponent*256 + int(v)
		}
		modulus := new(big.Int).SetBytes(n)
		if j.Kid == "" || modulus.BitLen() < 2048 || modulus.BitLen() > 8192 || exponent < 3 || exponent%2 == 0 || keys[j.Kid] != nil {
			return ErrProtocol
		}
		keys[j.Kid] = &rsa.PublicKey{N: modulus, E: exponent}
	}
	if len(keys) == 0 {
		return ErrProtocol
	}
	c.mu.Lock()
	c.keys = keys
	c.keysExpires = c.now().Add(5 * time.Minute)
	c.lastKeyRefresh = c.now()
	c.mu.Unlock()
	return nil
}
func (c *Client) verify(ctx context.Context, token, nonce string, refresh bool) (claims, error) {
	var out claims
	if len(token) > 65536 {
		return out, ErrLogin
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return out, ErrLogin
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return out, ErrLogin
	}
	var h struct {
		Alg  string   `json:"alg"`
		Kid  string   `json:"kid"`
		Crit []string `json:"crit"`
	}
	if json.Unmarshal(header, &h) != nil || h.Alg != "RS256" || h.Kid == "" || len(h.Crit) > 0 {
		return out, ErrLogin
	}
	k, err := c.verificationKey(ctx, h.Kid)
	if err != nil {
		return out, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return out, ErrLogin
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], signature) != nil {
		return out, ErrLogin
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(payload, &out) != nil {
		return out, ErrLogin
	}
	now := c.now().Unix()
	if out.Issuer != c.config.Issuer || out.Subject == "" || len(out.Subject) > 255 || out.Expires <= now || out.Issued <= 0 || out.Issued > now+30 || out.Issued > out.Expires || out.NotBefore > now+30 {
		return out, ErrLogin
	}
	var audience []string
	var single string
	if json.Unmarshal(out.Audience, &single) == nil {
		audience = []string{single}
	} else if json.Unmarshal(out.Audience, &audience) != nil {
		return out, ErrLogin
	}
	if !contains(audience, c.config.ClientID) || (len(audience) > 1 && out.AuthorizedParty != c.config.ClientID) || (out.AuthorizedParty != "" && out.AuthorizedParty != c.config.ClientID) {
		return out, ErrLogin
	}
	if (!refresh || out.Nonce != "") && out.Nonce != nonce {
		return out, ErrLogin
	}
	return out, nil
}

// Expired-cache callers share one fetch instead of treating another caller's
// in-flight fetch as an invalid identity. Waiters retain their own cancellation.
func (c *Client) verificationKey(ctx context.Context, kid string) (key, error) {
	for {
		c.mu.Lock()
		now := c.now()
		k := c.keys[kid]
		expired := !c.keysExpires.After(now)
		if k != nil && !expired {
			c.mu.Unlock()
			return k, nil
		}
		if flight := c.keyFlight; flight != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ErrUnavailable
			case <-flight:
			}
			continue
		}
		if now.Sub(c.lastKeyRefresh) < time.Second {
			err := ErrLogin
			if c.keyError != nil {
				err = c.keyError
			}
			c.mu.Unlock()
			return nil, err
		}
		flight := make(chan struct{})
		c.keyFlight = flight
		c.lastKeyRefresh = now
		c.mu.Unlock()
		err := c.loadKeys(ctx)
		c.mu.Lock()
		c.keyError = err
		c.lastKeyRefresh = c.now()
		c.keyFlight = nil
		close(flight)
		k = c.keys[kid]
		expired = !c.keysExpires.After(c.now())
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if k == nil || expired {
			return nil, ErrLogin
		}
		return k, nil
	}
}
