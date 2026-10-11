package console

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/mc/internal/oidc"
)

// OIDCConfig contains only administrator-selected endpoints and a client factory.
// The factory must include the STS session token; no startup identity is borrowed.
type OIDCConfig struct {
	Client        *oidc.Client
	StorageTarget string
	Connect       func(context.Context, oidc.Credentials) (consoleapi.Backend, func(), error)
}
type oidcSession struct {
	grant       *oidc.Grant
	generations *oidcGenerations
}
type oidcRequestKey struct{}
type oidcRequestSession struct {
	sess *session
	hash [32]byte
}

const oidcBindingCookie = "__Host-oc_oidc_binding"

func sameSession(a, b *session) bool {
	return a == b || (a != nil && b != nil && a.oidc != nil && a.oidc == b.oidc)
}
func oidcCookieHash(r *http.Request, name string) [32]byte {
	c, err := r.Cookie(name)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256([]byte(c.Value))
}
func oidcCookie(value string, age int) *http.Cookie {
	return &http.Cookie{Name: oidcBindingCookie, Value: value, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: age}
}

func (s *Server) oidcRuntime(ctx context.Context, credentials oidc.Credentials, identity string) (sessionRuntime, error) {
	if err := tryAcquire(ctx, s.loginSlots); err != nil {
		return sessionRuntime{}, err
	}
	defer func() { <-s.loginSlots }()
	s.mu.Lock()
	if s.closed || ctx.Err() != nil {
		s.mu.Unlock()
		return sessionRuntime{}, errOIDCSession
	}
	s.clients.Add(1)
	s.mu.Unlock()
	backend, cleanup, err := s.oidc.Connect(ctx, credentials)
	if err != nil || backend == nil || cleanup == nil || ctx.Err() != nil {
		if cleanup != nil {
			cleanup()
		}
		s.clients.Done()
		return sessionRuntime{}, errOIDCSession
	}
	runtime := sessionRuntime{backend: backend, identity: identity}
	if s.allowWrites {
		var ok bool
		runtime.writer, ok = backend.(consoleapi.MutationBackend)
		if !ok {
			cleanup()
			s.clients.Done()
			return sessionRuntime{}, errOIDCSession
		}
	}
	runtime.settings, _ = backend.(consoleapi.SettingsBackend)
	runtime.lifetime = &backendLifetime{cleanup: func() { defer s.clients.Done(); cleanup() }}
	return runtime, nil
}

func (s *Server) serveOIDC(w http.ResponseWriter, r *http.Request) {
	if !s.requireMethod(w, r, http.MethodGet) {
		return
	}
	if err := tryAcquire(r.Context(), s.verificationSlots); err != nil {
		writeSlotError(w, err)
		return
	}
	defer func() { <-s.verificationSlots }()
	s.mu.Lock()
	if time.Since(s.loginWindow) >= time.Minute {
		s.loginWindow, s.loginCount = time.Now(), 0
	}
	s.loginCount++
	tooMany := s.loginCount > maxLoginAttempts
	s.mu.Unlock()
	if tooMany {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "too_many_attempts", "Wait before signing in again.")
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(r.URL.RawQuery) > maxQueryLength {
		writeError(w, 400, "invalid_callback", "Start a new sign-in.")
		return
	}
	if r.URL.Path == "/api/oidc/login" {
		if len(q) != 0 {
			writeError(w, 400, "invalid_input", "Start a new sign-in.")
			return
		}
		binding, err := randomToken()
		if err != nil {
			writeError(w, 503, "login_unavailable", "Sign-in unavailable.")
			return
		}
		auth, err := s.oidc.Client.Begin(binding)
		if err != nil {
			writeError(w, 503, "login_unavailable", "Sign-in unavailable.")
			return
		}
		http.SetCookie(w, oidcCookie(binding, 300))
		http.Redirect(w, r, auth.URL, http.StatusSeeOther)
		return
	}
	// Keycloak adds session_state and iss. Validate any issuer against the verified
	// grant below; only single-valued standard callback metadata is accepted.
	for name, values := range q {
		if len(values) != 1 || (name != "code" && name != "state" && name != "session_state" && name != "iss") {
			writeError(w, 400, "invalid_callback", "Start a new sign-in.")
			return
		}
	}
	cookies := 0
	binding := ""
	for _, c := range r.Cookies() {
		if c.Name == oidcBindingCookie {
			cookies++
			binding = c.Value
		}
	}
	if cookies != 1 || len(binding) != 43 || q.Get("state") == "" || q.Get("code") == "" {
		writeError(w, 400, "invalid_callback", "Start a new sign-in.")
		return
	}
	http.SetCookie(w, oidcCookie("", -1))
	grant, err := s.oidc.Client.Complete(r.Context(), binding, q.Get("state"), q.Get("code"))
	if err != nil {
		writeError(w, 401, "login_rejected", "Sign-in failed. Start a new sign-in.")
		return
	}
	accepted := false
	defer func() {
		if !accepted {
			grant.Close()
		}
	}()
	issuer, subject := grant.Identity()
	if q.Get("iss") != "" && q.Get("iss") != issuer {
		writeError(w, 401, "login_rejected", "Sign-in failed.")
		return
	}
	identity, err := OIDCPrincipalIdentity(s.oidc.StorageTarget, issuer, subject)
	credentials, credErr := grant.Credentials()
	if err != nil || credErr != nil {
		writeError(w, 401, "login_rejected", "Sign-in failed.")
		return
	}
	runtime, err := s.oidcRuntime(r.Context(), credentials, identity)
	if err != nil {
		writeError(w, 503, "connection_unavailable", "Storage connection unavailable.")
		return
	}
	token, err := randomToken()
	if err != nil {
		runtime.retire()
		writeError(w, 503, "login_unavailable", "Sign-in unavailable.")
		return
	}
	csrf, err := randomToken()
	if err != nil {
		runtime.retire()
		writeError(w, 503, "login_unavailable", "Sign-in unavailable.")
		return
	}
	deadline := time.Now().Add(s.ttl)
	manager := newOIDCGenerations(s.life, runtime, credentials.Expires, deadline)
	sess := &session{runtime: runtime, csrf: csrf, expires: deadline, ctx: manager.ctx, cancel: manager.cancel, oidc: &oidcSession{grant: grant, generations: manager}}
	hash := sha256.Sum256([]byte(token))
	s.mu.Lock()
	if s.closed || len(s.sessions) >= maxSessions || r.Context().Err() != nil || manager.ctx.Err() != nil {
		s.mu.Unlock()
		manager.close()
		writeError(w, 503, "login_unavailable", "Sign-in unavailable.")
		return
	}
	entry := s.userPreferences[identity]
	if entry == nil {
		store, e := newPreferenceStore(s.dataDir, identity)
		if e != nil {
			s.mu.Unlock()
			manager.close()
			writeError(w, 503, "preferences_unavailable", "Preferences unavailable.")
			return
		}
		entry = &principalPreferences{store: store}
		s.userPreferences[identity] = entry
	}
	entry.refs++
	sess.runtime.preferences = entry.store
	manager.current.runtime.preferences = entry.store
	cleanup := manager.current.runtime.lifetime.cleanup
	manager.current.runtime.lifetime.cleanup = func() { s.releasePrincipalPreferences(identity, entry); cleanup() }
	sess.runtime = manager.current.runtime
	s.sessions[hash] = sess
	s.workers.Add(1)
	s.mu.Unlock()
	accepted = true
	go s.watchOIDC(hash, sess)
	http.SetCookie(w, s.sessionCookie(token, 0))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) watchOIDC(hash [32]byte, sess *session) {
	defer s.workers.Done()
	manager := sess.oidc.generations
	defer func() { sess.oidc.grant.Close(); s.mu.Lock(); s.removeSessionLocked(hash, sess); s.mu.Unlock() }()
	for {
		manager.mu.Lock()
		g := manager.current
		manager.mu.Unlock()
		remaining := time.Until(g.expires)
		advance := remaining / 3
		if advance > 30*time.Second {
			advance = 30 * time.Second
		}
		wait := time.NewTimer(remaining - advance)
		select {
		case <-manager.ctx.Done():
			wait.Stop()
			return
		case <-g.ctx.Done():
			wait.Stop()
			return
		case <-wait.C:
		}
		err := manager.refresh(manager.ctx, func(ctx context.Context) (sessionRuntime, time.Time, error) {
			credentials, err := sess.oidc.grant.Refresh(ctx)
			if err != nil {
				return sessionRuntime{}, time.Time{}, err
			}
			issuer, subject := sess.oidc.grant.Identity()
			identity, err := OIDCPrincipalIdentity(s.oidc.StorageTarget, issuer, subject)
			if err != nil || identity != g.runtime.identity {
				return sessionRuntime{}, time.Time{}, errors.New("OIDC principal changed")
			}
			runtime, err := s.oidcRuntime(ctx, credentials, identity)
			if err != nil {
				return sessionRuntime{}, time.Time{}, err
			}
			s.mu.Lock()
			entry := s.userPreferences[identity]
			if s.closed || !sameSession(s.sessions[hash], sess) || entry == nil {
				s.mu.Unlock()
				runtime.retire()
				return sessionRuntime{}, time.Time{}, errOIDCSession
			}
			entry.refs++
			runtime.preferences = entry.store
			cleanup := runtime.lifetime.cleanup
			runtime.lifetime.cleanup = func() { s.releasePrincipalPreferences(identity, entry); cleanup() }
			s.mu.Unlock()
			return runtime, credentials.Expires, nil
		})
		if err != nil {
			return
		}
		// Do not spin near the immutable deadline when refreshed credentials cannot
		// extend the current expiry. That lifetime simply ends at its fixed boundary.
		manager.mu.Lock()
		next := manager.current
		manager.mu.Unlock()
		if !next.expires.After(g.expires) || !next.expires.Before(manager.deadline) {
			select {
			case <-next.ctx.Done():
			case <-manager.ctx.Done():
			}
			return
		}
	}
}
