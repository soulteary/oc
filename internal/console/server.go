// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

// Package console serves an optional, loopback-only storage console.
package console

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/soulteary/mc/internal/clienttransport"
	"github.com/soulteary/mc/internal/console/web"
	"github.com/soulteary/mc/internal/consoleapi"
)

const (
	nativeCookieName = "__Host-oc_console_session"
	cookieName       = "oc_console_session"
	maxSessions      = 16
	maxLoginBody     = 1024
	maxQueryLength   = 24 * 1024
	maxLoginAttempts = 30
)

type NativeCredentials struct {
	AccessKey string
	SecretKey string
}

// NativeConnection must contain a verified, owned backend and a stable hashed
// target/user identity. Credentials and endpoints are never returned to browsers.
type NativeConnection struct {
	Backend  consoleapi.Backend
	Cleanup  func()
	Identity string
}

type Config struct {
	NativeLogin func(context.Context, NativeCredentials) (NativeConnection, error)
	DataDir     string
	Identity    string
	Backend     consoleapi.Backend
	// BackendFactory creates a fresh, session-owned client after code validation.
	// Backend remains the startup capability check and borrowed compatibility path.
	// A successful factory must return a nonnil backend and cleanup callback.
	BackendFactory func(context.Context) (consoleapi.Backend, func(), error)
	Alias          string
	BaseURL        string
	LoginCode      string
	SessionTTL     time.Duration
	AllowWrites    bool
	MaxUploadSize  int64
	AllowSharing   bool
	MaxArchiveSize int64
	ArchiveDir     string
}

// sessionRuntime is selected at sign-in and never replaced for an active session.
// A nil lifetime denotes the compatibility path borrowing the startup client.
type sessionRuntime struct {
	backend     consoleapi.Backend
	writer      consoleapi.MutationBackend
	settings    consoleapi.SettingsBackend
	preferences *preferenceStore
	lifetime    *backendLifetime
	identity    string
}

type session struct {
	runtime sessionRuntime
	csrf    string
	expires time.Time
	ctx     context.Context
	cancel  context.CancelFunc
	timer   *time.Timer
}

type sessionReply struct {
	Alias         string `json:"alias"`
	CSRFToken     string `json:"csrfToken"`
	ReadOnly      bool   `json:"readOnly"`
	MaxUploadSize int64  `json:"maxUploadSize"`
}

// Server owns clients returned by BackendFactory. A Config.Backend supplied
// without a factory is borrowed and must be closed by the caller after shutdown.
type Server struct {
	// Startup defaults are copied into a session at login. Request handlers and
	// background tasks must use that session's runtime instead of these fields.
	preferences     *preferenceStore
	backend         consoleapi.Backend
	backendFactory  func(context.Context) (consoleapi.Backend, func(), error)
	nativeLogin     func(context.Context, NativeCredentials) (NativeConnection, error)
	dataDir         string
	userPreferences map[string]*principalPreferences
	allowWrites     bool
	alias           string
	origin          string
	host            string
	code            [32]byte
	ttl             time.Duration
	assets          fs.FS

	mu             sync.Mutex
	closed         bool
	rotating       bool
	sessions       map[[32]byte]*session
	loginWindow    time.Time
	loginCount     int
	apiSlots       chan struct{}
	downloads      chan struct{}
	writer         consoleapi.MutationBackend
	settings       consoleapi.SettingsBackend
	maxUploadSize  int64
	jobs           map[string]*job
	writeSlots     chan struct{}
	planSlots      chan struct{}
	workers        sync.WaitGroup
	clients        sync.WaitGroup
	loginSlots     chan struct{}
	life           context.Context
	stopLife       context.CancelFunc
	streamIdle     time.Duration
	allowSharing   bool
	maxArchiveSize int64
	archiveDir     string
	archives       map[string]*archiveTask
	archiveSlots   chan struct{}
}

func New(cfg Config) (*Server, error) {
	native := cfg.NativeLogin != nil
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u == nil || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Hostname() == "" {
		return nil, errors.New("console base URL must be a root browser origin")
	}
	if err := clienttransport.ValidateEndpointHost(u); err != nil {
		return nil, err
	}
	if native {
		if u.Scheme != "https" || cfg.Backend != nil || cfg.BackendFactory != nil || cfg.LoginCode != "" || cfg.AllowWrites {
			return nil, errors.New("native login requires HTTPS, a dedicated authenticator and read-only storage access")
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsUnspecified() {
			return nil, errors.New("native browser origin must name a reachable host")
		}
	} else {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, errors.New("console base URL must use an HTTP loopback host")
		}
	}
	if u.Port() != "" || !native {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("console base URL must include a valid port")
		}
	}
	if (!native && (cfg.Backend == nil || cfg.LoginCode == "")) || cfg.Alias == "" || len(cfg.LoginCode) > 256 || cfg.SessionTTL < 0 {
		return nil, errors.New("console backend, alias, login code and a valid session duration are required")
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 30 * time.Minute
	}
	if cfg.MaxUploadSize == 0 {
		cfg.MaxUploadSize = 1 << 30
	}
	if cfg.MaxUploadSize < 0 || cfg.MaxUploadSize > 5<<30 {
		return nil, errors.New("console upload limit must be between one byte and five GiB")
	}
	if cfg.MaxArchiveSize == 0 {
		cfg.MaxArchiveSize = 5 << 30
	}
	if cfg.MaxArchiveSize < 1 || cfg.MaxArchiveSize > 5<<30 {
		return nil, errors.New("console archive limit must be between one byte and five GiB")
	}
	var writer consoleapi.MutationBackend
	if cfg.AllowWrites {
		var ok bool
		writer, ok = cfg.Backend.(consoleapi.MutationBackend)
		if !ok {
			return nil, errors.New("console backend does not support writes")
		}
	}
	var preferences *preferenceStore
	if !native {
		preferences, err = newPreferenceStore(cfg.DataDir, cfg.Identity)
		if err != nil {
			return nil, err
		}
	}
	life, stopLife := context.WithCancel(context.Background())
	settings, _ := cfg.Backend.(consoleapi.SettingsBackend)
	return &Server{
		nativeLogin: cfg.NativeLogin, dataDir: cfg.DataDir, userPreferences: make(map[string]*principalPreferences), preferences: preferences, backend: cfg.Backend, backendFactory: cfg.BackendFactory, allowWrites: cfg.AllowWrites, alias: cfg.Alias, origin: u.Scheme + "://" + u.Host,
		host: u.Host, code: sha256.Sum256([]byte(cfg.LoginCode)), ttl: cfg.SessionTTL,
		assets: web.FS(), sessions: make(map[[32]byte]*session),
		apiSlots: make(chan struct{}, 8), downloads: make(chan struct{}, 2),
		loginSlots: make(chan struct{}, 2),
		writer:     writer, settings: settings, maxUploadSize: cfg.MaxUploadSize, jobs: make(map[string]*job), writeSlots: make(chan struct{}, 2), planSlots: make(chan struct{}, 2), life: life, stopLife: stopLife, streamIdle: 30 * time.Second,
		allowSharing: cfg.AllowSharing, maxArchiveSize: cfg.MaxArchiveSize, archiveDir: cfg.ArchiveDir, archives: make(map[string]*archiveTask), archiveSlots: make(chan struct{}, 1),
	}, nil
}

// Close invalidates sessions and cancels any requests using those sessions.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.stopLife()
	for token, sess := range s.sessions {
		s.removeSessionLocked(token, sess)
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Release the client after request/body cancellation and all handler cleanup.
	var releaseClient func()
	defer func() {
		if releaseClient != nil {
			releaseClient()
		}
	}()
	requestCtx, requestCancel := context.WithCancel(r.Context())
	stopServer := context.AfterFunc(s.life, requestCancel)
	defer func() { stopServer(); requestCancel() }()
	r = r.WithContext(requestCtx)
	w = &contextResponseWriter{ResponseWriter: w, ctx: requestCtx}
	// Prevent net/http's post-handler body drain from blocking after an early
	// rejection, even when the listener has no whole-request ReadTimeout.
	if r.Body != nil && r.Body != http.NoBody {
		body := &trackedRequestBody{ReadCloser: r.Body}
		r.Body = body
		defer func() {
			controller := http.NewResponseController(w)
			if bodyNeedsInterrupt(body) {
				_ = controller.SetReadDeadline(time.Now())
			}
			_ = body.Close()
			_ = controller.SetReadDeadline(time.Time{})
		}()
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' blob:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; object-src 'none'")
	if s.nativeLogin != nil && r.TLS == nil {
		writeError(w, http.StatusBadRequest, "https_required", "Use the HTTPS console URL.")
		return
	}
	if r.Host != s.host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.origin) || len(r.Header.Values("Origin")) > 1 {
		writeError(w, http.StatusForbidden, "invalid_origin", "Open the console using its local URL.")
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		writeError(w, http.StatusForbidden, "invalid_origin", "Cross-site requests are not allowed.")
		return
	}
	s.mu.Lock()
	closed := s.closed
	rotating := s.rotating
	s.mu.Unlock()
	if closed {
		writeError(w, http.StatusServiceUnavailable, "console_closed", "The console has stopped.")
		return
	}
	// Logout must remain available while writes are paused. It revokes only
	// this browser session and cancels its request; Origin and CSRF checks
	// still run below. Storage actions and new logins remain blocked.
	if rotating && r.URL.Path != "/api/logout" {
		writeError(w, http.StatusServiceUnavailable, "credential_change_pending", "The account secret is being changed. Wait for the result before restarting OC.")
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		s.serveAsset(w, r)
		return
	}
	if r.URL.Path == "/api/login" {
		if !s.requireMethod(w, r, http.MethodPost) || !s.requireOrigin(w, r) {
			return
		}
		s.login(w, r)
		return
	}
	// Hold the selected client's transport through this entire request, including
	// the response stream. Reauthentication in route handlers still checks expiry.
	if sess, _ := s.authenticate(r); sess != nil {
		if !sess.runtime.retain() {
			writeError(w, http.StatusUnauthorized, "login_required", s.loginPrompt())
			return
		}
		releaseClient = sess.runtime.release
	}
	if s.isJobRoute(r.URL.Path) {
		s.serveJobs(w, r)
		return
	}
	if s.isSettingsRoute(r.URL.Path) {
		s.serveSettings(w, r)
		return
	}
	if s.isFeatureRoute(r.URL.Path) {
		s.serveFeatures(w, r)
		return
	}
	if s.isIAMRoute(r.URL.Path) {
		s.serveIAM(w, r)
		return
	}
	if s.isArchiveRoute(r.URL.Path) {
		s.serveArchives(w, r)
		return
	}
	if r.URL.Path == "/api/preferences" {
		s.servePreferences(w, r)
		return
	}
	method := http.MethodGet
	switch r.URL.Path {
	case "/api/logout":
		method = http.MethodPost
	case "/api/session", "/api/buckets", "/api/objects", "/api/account", "/api/download", "/api/capabilities", "/api/object-info":
	default:
		writeError(w, http.StatusNotFound, "not_found", "This console endpoint does not exist.")
		return
	}
	if !s.requireMethod(w, r, method) {
		return
	}
	sess, token := s.authenticate(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "login_required", s.loginPrompt())
		return
	}
	if r.URL.Path == "/api/logout" {
		if !s.requireOrigin(w, r) {
			return
		}
		if !s.requireCSRF(w, r, sess) {
			return
		}
		s.mu.Lock()
		s.removeSessionLocked(token, sess)
		s.mu.Unlock()
		http.SetCookie(w, s.sessionCookie("", -1))
		writePayload(w, http.StatusNoContent, nil)
		return
	}
	if r.URL.Path == "/api/session" {
		writeJSON(w, http.StatusOK, sessionReply{Alias: s.alias, CSRFToken: sess.csrf, ReadOnly: sess.runtime.writer == nil, MaxUploadSize: s.maxUploadSize})
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(sess.ctx, cancel)
	defer func() { stop(); cancel() }()
	if r.URL.Path != "/api/download" {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, 30*time.Second)
		defer timeoutCancel()
	}
	if !acquire(w, ctx, s.apiSlots) {
		return
	}
	defer func() { <-s.apiSlots }()
	if len(r.URL.RawQuery) > maxQueryLength {
		writeError(w, http.StatusBadRequest, "invalid_input", "The request is too long.")
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "The request parameters are invalid.")
		return
	}
	switch r.URL.Path {
	case "/api/capabilities":
		_, buckets := sess.runtime.backend.(consoleapi.BucketBackend)
		_, versions := sess.runtime.backend.(consoleapi.VersionBackend)
		if len(q) > 1 || len(q["bucket"]) > 1 || (len(q) == 1 && len(q["bucket"]) != 1) || (q.Get("bucket") != "" && !validBucket(q.Get("bucket"))) {
			writeError(w, 400, "invalid_input", "Choose one bucket for capability detection.")
			return
		}
		if detector, ok := sess.runtime.backend.(consoleapi.VersionCapabilityBackend); ok {
			versions = false
			if q.Get("bucket") != "" {
				supported, err := detector.VersionSupported(ctx, q.Get("bucket"))
				versions = err == nil && supported
			}
		}
		_, rename := sess.runtime.backend.(consoleapi.RenameBackend)
		_, shares := sess.runtime.backend.(consoleapi.ShareBackend)
		_, archives := sess.runtime.backend.(consoleapi.ReferenceBackend)
		_, iam := sess.runtime.backend.(consoleapi.IAMBackend)
		// These describe implemented interfaces and explicit process gates.
		// Individual reads still discover server protocols and permissions.
		writeJSON(w, 200, map[string]any{"rename": rename && sess.runtime.writer != nil, "bucketManagement": buckets, "versions": versions, "versioning": versions, "sharing": shares && s.allowSharing, "archives": archives, "iam": iam, "iamPolicyBindings": iam, "writesAllowed": sess.runtime.writer != nil, "maxArchiveSize": s.maxArchiveSize, "maxArchiveObjects": maxPlanKeys})
	case "/api/buckets":
		buckets, err := sess.runtime.backend.ListBuckets(ctx)
		if err != nil {
			writeBackendError(w, err)
			return
		}
		if buckets == nil {
			buckets = []consoleapi.Bucket{}
		}
		writeJSON(w, http.StatusOK, struct {
			Buckets []consoleapi.Bucket `json:"buckets"`
		}{buckets})
	case "/api/account":
		account, err := sess.runtime.backend.AccountInfo(ctx)
		if err != nil {
			writeBackendError(w, err)
			return
		}
		if account.Buckets == nil {
			account.Buckets = []consoleapi.AccountBucket{}
		}
		writeJSON(w, http.StatusOK, account)
	case "/api/objects":
		bucket, prefix, cursor, limit, ok := objectQuery(q)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_input", "The bucket, prefix, cursor or page size is invalid.")
			return
		}
		page, err := sess.runtime.backend.ListObjects(ctx, bucket, prefix, cursor, limit)
		if err != nil {
			writeBackendError(w, err)
			return
		}
		if page.Entries == nil {
			page.Entries = []consoleapi.Entry{}
		}
		writeJSON(w, http.StatusOK, page)
	case "/api/object-info":
		bucket, key := q.Get("bucket"), q.Get("key")
		if !validBucket(bucket) || key == "" || !validKey(key) || len(q["bucket"]) != 1 || len(q["key"]) != 1 || len(q) != 2 {
			writeError(w, 400, "invalid_input", "Choose an exact bucket and object key.")
			return
		}
		backend, ok := sess.runtime.backend.(consoleapi.ReferenceBackend)
		if !ok {
			writeError(w, 501, "metadata_unsupported", "Object information is unavailable on this connection.")
			return
		}
		info, err := backend.StatReference(ctx, consoleapi.ObjectRef{Bucket: bucket, Key: key})
		if err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, 200, info)

	case "/api/download":
		bucket, key := q.Get("bucket"), q.Get("key")
		if !validBucket(bucket) || key == "" || !validKey(key) || len(q["bucket"]) != 1 || len(q["key"]) != 1 || len(q["versionId"]) > 1 || !validVersionID(q.Get("versionId")) || len(q) > 3 {
			writeError(w, http.StatusBadRequest, "invalid_input", "The bucket or object key is invalid.")
			return
		}
		if !acquire(w, ctx, s.downloads) {
			return
		}
		defer func() { <-s.downloads }()
		if versionID := q.Get("versionId"); versionID != "" {
			s.downloadReference(w, ctx, sess, consoleapi.ObjectRef{Bucket: bucket, Key: key, VersionID: versionID})
		} else {
			s.download(w, ctx, sess, bucket, key)
		}
	}
}

func (s *Server) requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "This request method is not supported.")
	return false
}

func (s *Server) requireOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") == s.origin {
		return true
	}
	writeError(w, http.StatusForbidden, "invalid_origin", "Use the console page for this operation.")
	return false
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "This operation is not supported by the read-only console.")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if name != "index.html" && name != "app.js" && name != "i18n.js" && name != "style.css" {
		writeError(w, http.StatusNotFound, "not_found", "This console page does not exist.")
		return
	}
	data, err := fs.ReadFile(s.assets, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "This console page does not exist.")
		return
	}
	if name == "index.html" && s.nativeLogin != nil {
		data = []byte(strings.Replace(string(data), `data-auth-mode="local"`, `data-auth-mode="native"`, 1))
	}
	contentTypes := map[string]string{"index.html": "text/html; charset=utf-8", "app.js": "text/javascript; charset=utf-8", "i18n.js": "text/javascript; charset=utf-8", "style.css": "text/css; charset=utf-8"}
	w.Header().Set("Content-Type", contentTypes[name])
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodGet {
		writePayload(w, http.StatusOK, data)
	} else {
		w.WriteHeader(http.StatusOK)
		writePayload(w, 0, nil)
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if time.Since(s.loginWindow) >= time.Minute {
		s.loginWindow, s.loginCount = time.Now(), 0
	}
	s.loginCount++
	tooMany := s.loginCount > maxLoginAttempts
	s.mu.Unlock()
	if tooMany {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "Wait a minute before trying to sign in again.")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_input", "Sign-in requires a JSON request.")
		return
	}
	var args struct {
		Code      string `json:"code"`
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
	}
	if !decodeJSON(w, r, &args, maxLoginBody) {
		return
	}
	codeHash := sha256.Sum256([]byte(args.Code))
	native := s.nativeLogin != nil
	if (native && (args.Code != "" || !validNativeCredentials(NativeCredentials{AccessKey: args.AccessKey, SecretKey: args.SecretKey}))) ||
		(!native && (args.AccessKey != "" || args.SecretKey != "" || subtle.ConstantTimeCompare(codeHash[:], s.code[:]) != 1)) {
		if native {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Sign in with an enabled native IAM user and its current secret.")
		} else {
			writeError(w, http.StatusUnauthorized, "invalid_login_code", "The sign-in code is incorrect.")
		}
		return
	}
	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create a session.")
		return
	}
	csrf, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create a session.")
		return
	}
	hash := sha256.Sum256([]byte(token))
	runtime, err := s.newSessionRuntime(r.Context(), NativeCredentials{AccessKey: args.AccessKey, SecretKey: args.SecretKey})
	args.SecretKey = ""
	if err != nil {
		var failure *consoleapi.Error
		if native && errors.As(err, &failure) && failure != nil && failure.Code == "AccessDenied" {
			writeError(w, 401, "invalid_credentials", "Sign in with an enabled native IAM user and its current secret.")
		} else {
			writeError(w, http.StatusServiceUnavailable, "connection_unavailable", "Unable to verify a storage connection. Check server support and try signing in again.")
		}
		return
	}
	ctx, cancel := context.WithCancel(s.life)
	sess := &session{runtime: runtime, csrf: csrf, expires: time.Now().Add(s.ttl), ctx: ctx, cancel: cancel}
	s.mu.Lock()
	// A login can finish reading its body after another request starts
	// rotating credentials. Recheck the pause at the session commit point.
	if s.closed || s.rotating || len(s.sessions) >= maxSessions || r.Context().Err() != nil {
		closed := s.closed
		rotating := s.rotating
		s.mu.Unlock()
		cancel()
		runtime.retire()
		if closed {
			writeError(w, http.StatusServiceUnavailable, "console_closed", "The console has stopped.")
		} else if rotating {
			writeError(w, http.StatusServiceUnavailable, "credential_change_pending", "The account secret is being changed. Wait for the result before restarting OC.")
		} else if r.Context().Err() != nil {
			writeError(w, http.StatusServiceUnavailable, "connection_unavailable", "The sign-in request stopped.")
		} else {
			writeError(w, http.StatusTooManyRequests, "too_many_sessions", "Too many console sessions are open. Close an existing session or wait for it to expire.")
		}
		return
	}
	if native {
		count := 0
		for _, existing := range s.sessions {
			if existing.runtime.identity == runtime.identity {
				count++
			}
		}
		if count >= 4 {
			s.mu.Unlock()
			cancel()
			runtime.retire()
			writeError(w, 429, "too_many_sessions", "This user has too many open sessions.")
			return
		}
		entry := s.userPreferences[runtime.identity]
		if entry == nil {
			preferences, loadErr := newPreferenceStore(s.dataDir, runtime.identity)
			if loadErr != nil {
				s.mu.Unlock()
				cancel()
				runtime.retire()
				writeError(w, 503, "preferences_unavailable", "Unable to load user preferences.")
				return
			}
			entry = &principalPreferences{store: preferences}
			s.userPreferences[runtime.identity] = entry
		}
		entry.refs++
		sess.runtime.preferences = entry.store
		cleanup := sess.runtime.lifetime.cleanup
		sess.runtime.lifetime.cleanup = func() {
			s.releasePrincipalPreferences(runtime.identity, entry)
			cleanup()
		}
	}
	s.sessions[hash] = sess
	sess.timer = time.AfterFunc(s.ttl, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.removeSessionLocked(hash, sess)
	})
	s.mu.Unlock()
	http.SetCookie(w, s.sessionCookie(token, 0))
	writeJSON(w, http.StatusOK, sessionReply{Alias: s.alias, CSRFToken: csrf, ReadOnly: sess.runtime.writer == nil, MaxUploadSize: s.maxUploadSize})
}

func (s *Server) removeSessionLocked(token [32]byte, sess *session) {
	if s.sessions[token] != sess {
		return
	}
	delete(s.sessions, token)
	if sess.timer != nil {
		sess.timer.Stop()
	}
	sess.cancel()
	for id, task := range s.jobs {
		if task.owner == sess {
			task.cancel()
			task.timer.Stop()
			delete(s.jobs, id)
		}
	}
	for id, task := range s.archives {
		if task.owner == sess {
			if task.timer != nil {
				task.timer.Stop()
			}
			s.cancelArchiveLocked(task)
			delete(s.archives, id)
		}
	}

	sess.runtime.retire()
}

func (s *Server) authenticate(r *http.Request) (*session, [32]byte) {
	cookie, err := r.Cookie(s.cookieName())
	if err != nil || len(cookie.Value) != 43 {
		return nil, [32]byte{}
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[hash]
	if sess == nil || s.closed {
		return nil, hash
	}
	if !time.Now().Before(sess.expires) {
		s.removeSessionLocked(hash, sess)
		return nil, hash
	}
	return sess, hash
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func objectQuery(q url.Values) (bucket, prefix, cursor string, limit int, ok bool) {
	bucket, prefix, cursor, limit = q.Get("bucket"), q.Get("prefix"), q.Get("cursor"), 100
	if !validBucket(bucket) || !validKey(prefix) || !utf8.ValidString(cursor) || len(cursor) > 16*1024 || len(q["bucket"]) != 1 || len(q["prefix"]) > 1 || len(q["cursor"]) > 1 || len(q["limit"]) > 1 {
		return
	}
	if value := q.Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 1000 {
			return
		}
	}
	ok = true
	return
}

func validBucket(bucket string) bool {
	if bucket == "" || len(bucket) > 63 {
		return false
	}
	for _, char := range bucket {
		if !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && char != '.' && char != '-' {
			return false
		}
	}
	return true
}

func validKey(key string) bool {
	return utf8.ValidString(key) && len(key) <= 1024 && !strings.ContainsRune(key, 0)
}

func acquire(w http.ResponseWriter, ctx context.Context, slots chan struct{}) bool {
	if err := tryAcquire(ctx, slots); err != nil {
		writeSlotError(w, err)
		return false
	}
	return true
}

var errBusy = errors.New("console busy")

func tryAcquire(ctx context.Context, slots chan struct{}) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	select {
	case slots <- struct{}{}:
		if ctx.Err() != nil {
			<-slots
			return ctx.Err()
		}
		return nil
	default:
		return errBusy
	}
}

func writeSlotError(w http.ResponseWriter, err error) {
	if errors.Is(err, errBusy) {
		writeError(w, 429, "busy", "The console is busy. Try again shortly.")
	} else {
		writeError(w, 408, "request_canceled", "The request was canceled.")
	}
}

func (s *Server) download(w http.ResponseWriter, ctx context.Context, sess *session, bucket, key string) {
	s.downloadUsing(w, ctx, key, func(ctx context.Context) (consoleapi.Object, error) {
		return sess.runtime.backend.OpenObject(ctx, bucket, key)
	})
}

func (s *Server) downloadReference(w http.ResponseWriter, ctx context.Context, sess *session, ref consoleapi.ObjectRef) {
	reader, ok := sess.runtime.backend.(consoleapi.ReferenceBackend)
	if !ok {
		writeError(w, 501, "versions_unsupported", "This connection does not support version downloads.")
		return
	}
	s.downloadUsing(w, ctx, ref.Key, func(ctx context.Context) (consoleapi.Object, error) { return reader.OpenReference(ctx, ref, "") })
}

func (s *Server) downloadUsing(w http.ResponseWriter, ctx context.Context, key string, open func(context.Context) (consoleapi.Object, error)) bool {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	object, err := open(ctx)
	if err != nil {
		if object.Body != nil {
			_ = object.Body.Close()
		}
		writeBackendError(w, err)
		return false
	}
	if object.Body == nil {
		writeError(w, http.StatusBadGateway, "upstream_error", "Unable to open the object.")
		return false
	}
	var once sync.Once
	closeBody := func() { once.Do(func() { _ = object.Body.Close() }) }
	writer := &flushWriter{ctx: ctx, response: w, controller: http.NewResponseController(w)}
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(callbackDone)
		// Closing the upstream cannot interrupt a blocked downstream socket write.
		writer.cancelWrite()
		closeBody()
	})
	defer func() {
		if !stop() {
			<-callbackDone
		}
		closeBody()
		// Keep the final write deadline through net/http's finishRequest: an
		// unknown-size stream still needs its terminating chunk written there.
		// net/http clears the deadline after finishRequest, before connection reuse.
	}()
	if ctx.Err() != nil {
		writeError(w, http.StatusRequestTimeout, "request_canceled", "The request was canceled.")
		return false
	}
	filename := key[strings.LastIndex(key, "/")+1:]
	filename = strings.Map(func(char rune) rune {
		if char < 32 || char == 127 {
			return -1
		}
		return char
	}, filename)
	if filename == "" || filename == "." || filename == ".." {
		filename = "download"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	if object.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(object.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	// Errors after headers are sent terminate the stream; JSON must never be
	// appended to a partial download. The backend stream is closed on all paths.
	copied, copyErr := io.Copy(writer, readWithIdleTimeout{body: object.Body, idle: s.streamIdle, interrupt: func() { cancel(); closeBody() }})
	if copyErr == nil && object.Size >= 0 && copied != object.Size {
		copyErr = io.ErrUnexpectedEOF
	}
	if copyErr == nil {
		// Even an empty object must flush its headers under a write deadline.
		// This also renews the bound for net/http's final chunk after EOF.
		_, copyErr = writer.Write(nil)
	}
	if copyErr != nil || ctx.Err() != nil {
		// A truncated chunked download must not end with a successful final
		// chunk. Native HTTP writers support deadlines; in-memory test writers
		// have no connection to abort.
		if err := writer.controller.SetWriteDeadline(time.Now()); err == nil {
			panic(http.ErrAbortHandler)
		}
	}
	return copyErr == nil && ctx.Err() == nil
}

type flushWriter struct {
	ctx        context.Context
	response   http.ResponseWriter
	controller *http.ResponseController
	mu         sync.Mutex
}

func (w *flushWriter) cancelWrite() {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.controller.SetWriteDeadline(time.Now())
}

func (w *flushWriter) Write(p []byte) (int, error) {
	// Serialize deadline updates with cancellation. Never hold this lock while
	// writing: cancellation needs to interrupt an already-blocked Write/Flush.
	w.mu.Lock()
	if err := w.ctx.Err(); err != nil {
		w.mu.Unlock()
		return 0, err
	}
	_ = w.controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
	w.mu.Unlock()
	n, err := w.response.Write(p)
	if err == nil {
		flushErr := w.controller.Flush()
		if !errors.Is(flushErr, http.ErrNotSupported) {
			err = flushErr
		}
	}
	return n, err
}

func writeBackendError(w http.ResponseWriter, err error) {
	var apiError *consoleapi.Error
	if errors.As(err, &apiError) && apiError != nil {
		status := apiError.Status
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		writeError(w, status, apiError.Code, apiError.Message)
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusRequestTimeout, "request_canceled", "The request was canceled.")
		return
	}
	writeError(w, http.StatusBadGateway, "upstream_error", "Unable to complete the storage request.")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, consoleapi.Error{Code: code, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		status = http.StatusInternalServerError
		payload = []byte(`{"code":"internal_error","message":"Unable to create the response."}`)
	}
	payload = append(payload, '\n')
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	writePayload(w, status, payload)
}
