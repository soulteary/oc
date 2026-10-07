// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type endlessStream struct {
	closed chan struct{}
	once   sync.Once
	read   atomic.Int64
}

func (b *endlessStream) Read(p []byte) (int, error) {
	select {
	case <-b.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	for i := range p {
		p[i] = 'x'
	}
	b.read.Add(int64(len(p)))
	return len(p), nil
}

func (b *endlessStream) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestSlowDownloadSocketReleasedOnSessionCancellation(t *testing.T) {
	for _, action := range []string{"logout", "expiry", "close"} {
		t.Run(action, func(t *testing.T) {
			body := &endlessStream{closed: make(chan struct{})}
			backend := &fakeBackend{open: func(context.Context, string, string) (consoleapi.Object, error) {
				return consoleapi.Object{Body: body, Size: 1 << 30}, nil
			}}
			downloadDone := make(chan struct{})
			var handler *Server
			native := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/download" {
					defer close(downloadDone)
				}
				handler.ServeHTTP(w, r)
			}))
			ttl := 30 * time.Minute
			if action == "expiry" {
				ttl = time.Second
			}
			var err error
			handler, err = New(Config{Backend: backend, Alias: "store", BaseURL: "http://" + native.Listener.Addr().String(), LoginCode: "test-code", SessionTTL: ttl})
			if err != nil {
				t.Fatal(err)
			}
			native.Start()
			defer native.Close()
			defer handler.Close()
			client := native.Client()
			client.Timeout = 3 * time.Second
			login, err := http.NewRequest(http.MethodPost, native.URL+"/api/login", strings.NewReader(`{"code":"test-code"}`))
			if err != nil {
				t.Fatal(err)
			}
			login.Header.Set("Origin", native.URL)
			login.Header.Set("Content-Type", "application/json")
			response, err := client.Do(login)
			if err != nil {
				t.Fatal(err)
			}
			var session sessionReply
			if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			cookies := response.Cookies()
			if response.StatusCode != 200 || len(cookies) != 1 {
				t.Fatal("login failed")
			}
			connection, err := net.Dial("tcp", native.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if tcp, ok := connection.(*net.TCPConn); ok {
				_ = tcp.SetReadBuffer(1024)
			}
			_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, err = io.WriteString(connection, "GET /api/download?bucket=bucket&key=large HTTP/1.1\r\nHost: "+native.Listener.Addr().String()+"\r\nCookie: "+cookies[0].Name+"="+cookies[0].Value+"\r\n\r\n")
			if err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(connection)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					t.Fatal(err)
				}
				if line == "\r\n" {
					break
				}
			}
			// Stop reading the body. Observe a stalled sender before cancellation;
			// it must be blocked on the real downstream socket, not upstream Read.
			previous := body.read.Load()
			stalled := false
			for attempt := 0; attempt < 12; attempt++ {
				time.Sleep(25 * time.Millisecond)
				current := body.read.Load()
				if current > 0 && current == previous {
					stalled = true
					break
				}
				previous = current
			}
			if !stalled {
				t.Fatal("could not establish a slow receiver")
			}
			select {
			case <-downloadDone:
				t.Fatal("download finished before cancellation")
			default:
			}
			if action == "logout" {
				logout, err := http.NewRequest(http.MethodPost, native.URL+"/api/logout", nil)
				if err != nil {
					t.Fatal(err)
				}
				logout.Header.Set("Origin", native.URL)
				logout.Header.Set("X-CSRF-Token", session.CSRFToken)
				logout.AddCookie(cookies[0])
				reply, err := client.Do(logout)
				if err != nil {
					t.Fatal(err)
				}
				reply.Body.Close()
				if reply.StatusCode != 204 {
					t.Fatal("logout failed")
				}
			} else if action == "close" {
				_ = handler.Close()
			}
			select {
			case <-downloadDone:
			case <-time.After(2 * time.Second):
				t.Fatal("session cancellation did not interrupt a blocked socket write")
			}
			select {
			case <-body.closed:
			default:
				t.Fatal("upstream stream still open")
			}
			if len(handler.downloads) != 0 || len(handler.apiSlots) != 0 {
				t.Fatal("request slots leaked")
			}
		})
	}
}

const testOrigin = "http://127.0.0.1:19001"

type fakeBackend struct {
	calls   atomic.Int32
	list    func(context.Context) ([]consoleapi.Bucket, error)
	objects func(context.Context, string, string, string, int) (consoleapi.Page, error)
	open    func(context.Context, string, string) (consoleapi.Object, error)
	account func(context.Context) (consoleapi.Account, error)
}

func (b *fakeBackend) ListBuckets(ctx context.Context) ([]consoleapi.Bucket, error) {
	b.calls.Add(1)
	if b.list != nil {
		return b.list(ctx)
	}
	return []consoleapi.Bucket{{Name: "bucket"}}, nil
}

func (b *fakeBackend) ListObjects(ctx context.Context, bucket, prefix, cursor string, limit int) (consoleapi.Page, error) {
	b.calls.Add(1)
	if b.objects != nil {
		return b.objects(ctx, bucket, prefix, cursor, limit)
	}
	return consoleapi.Page{}, nil
}

func (b *fakeBackend) OpenObject(ctx context.Context, bucket, key string) (consoleapi.Object, error) {
	b.calls.Add(1)
	if b.open != nil {
		return b.open(ctx, bucket, key)
	}
	return consoleapi.Object{Body: io.NopCloser(strings.NewReader("object")), Size: 6}, nil
}

func (b *fakeBackend) AccountInfo(ctx context.Context) (consoleapi.Account, error) {
	b.calls.Add(1)
	if b.account != nil {
		return b.account(ctx)
	}
	return consoleapi.Account{}, nil
}

func testServer(t *testing.T, backend *fakeBackend, ttl time.Duration) *Server {
	t.Helper()
	s, err := New(Config{Backend: backend, Alias: "local", BaseURL: testOrigin, LoginCode: "test-login-code", SessionTTL: ttl})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testRequest(method, target, body string, cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest(method, testOrigin+target, strings.NewReader(body))
	if method == http.MethodPost {
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

func signIn(t *testing.T, s *Server) (*http.Cookie, sessionReply) {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodPost, "/api/login", `{"code":"test-login-code"}`, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %v", cookies)
	}
	var response sessionReply
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return cookies[0], response
}

func TestNewRequiresLocalHTTPOrigin(t *testing.T) {
	for _, baseURL := range []string{"", "http://example.com:19001", "http://0.0.0.0:19001", "http://192.168.1.1:19001", "https://127.0.0.1:19001", "http://127.0.0.1", "http://127.0.0.1:0", "http://user@127.0.0.1:19001", testOrigin + "/console", testOrigin + "?code=secret", testOrigin + "#fragment"} {
		t.Run(baseURL, func(t *testing.T) {
			_, err := New(Config{Backend: &fakeBackend{}, Alias: "local", LoginCode: "code", BaseURL: baseURL})
			if err == nil {
				t.Fatalf("accepted unsafe URL %q", baseURL)
			}
		})
	}
	for _, baseURL := range []string{testOrigin, "http://[::1]:19001", "http://localhost:19001"} {
		s, err := New(Config{Backend: &fakeBackend{}, Alias: "local", LoginCode: "code", BaseURL: baseURL})
		if err != nil {
			t.Fatal(err)
		}
		if s.ttl != 30*time.Minute {
			t.Fatalf("unexpected default session TTL %v", s.ttl)
		}
		_ = s.Close()
	}
}

func TestAuthenticationPreventsStorageCalls(t *testing.T) {
	b := &fakeBackend{}
	s := testServer(t, b, 0)
	for _, path := range []string{"/api/session", "/api/buckets", "/api/objects?bucket=bucket", "/api/account", "/api/download?bucket=bucket&key=file"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, testRequest(http.MethodGet, path, "", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s returned %d", path, w.Code)
		}
	}
	if b.calls.Load() != 0 {
		t.Fatal("unauthenticated request reached backend")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/unknown", "", nil))
	if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "<!DOCTYPE") {
		t.Fatal("unknown API received a page fallback")
	}
}

func TestSessionCookieSecurityAndLogout(t *testing.T) {
	s := testServer(t, &fakeBackend{}, 0)
	cookie, response := signIn(t, s)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" || len(cookie.Value) != 43 || len(response.CSRFToken) != 43 || !response.ReadOnly || response.Alias != "local" {
		t.Fatalf("insecure or invalid session: cookie=%+v response=%+v", cookie, response)
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	s.mu.Lock()
	_, storedHash := s.sessions[hash]
	s.mu.Unlock()
	if !storedHash {
		t.Fatal("session was not stored by its token digest")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/session", "", cookie))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), response.CSRFToken) {
		t.Fatalf("session read failed: %d %s", w.Code, w.Body.String())
	}
	for _, csrf := range []string{"", "forged"} {
		r := testRequest(http.MethodPost, "/api/logout", "", cookie)
		r.Header.Set("X-CSRF-Token", csrf)
		w = httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("accepted invalid CSRF %q", csrf)
		}
	}
	r := testRequest(http.MethodPost, "/api/logout", "", cookie)
	r.Header.Set("X-CSRF-Token", response.CSRFToken)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout failed: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/account", "", cookie))
	if w.Code != http.StatusUnauthorized {
		t.Fatal("logged-out session remained usable")
	}
	newCookie, newResponse := signIn(t, s)
	if newCookie.Value == cookie.Value || newResponse.CSRFToken == response.CSRFToken {
		t.Fatal("re-login reused session credentials")
	}
}

func TestHostOriginAndFetchSiteGuards(t *testing.T) {
	b := &fakeBackend{}
	s := testServer(t, b, 0)
	cookie, _ := signIn(t, s)
	checks := []struct {
		name   string
		modify func(*http.Request)
	}{
		{"rebound host", func(r *http.Request) { r.Host = "evil.example:19001" }},
		{"different port", func(r *http.Request) { r.Host = "127.0.0.1:9000" }},
		{"other origin", func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }},
		{"same-site origin", func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:9000") }},
		{"null origin", func(r *http.Request) { r.Header.Set("Origin", "null") }},
		{"duplicate origin", func(r *http.Request) {
			r.Header.Add("Origin", testOrigin)
			r.Header.Add("Origin", "http://evil.example")
		}},
		{"cross-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }},
		{"same-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			r := testRequest(http.MethodGet, "/api/buckets", "", cookie)
			check.modify(r)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("request returned %d", w.Code)
			}
		})
	}
	if b.calls.Load() != 0 {
		t.Fatal("rejected browser request reached backend")
	}
	r := testRequest(http.MethodPost, "/api/login", `{"code":"test-login-code"}`, nil)
	r.Header.Del("Origin")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("login accepted absent origin")
	}
}

func TestLoginIsJSONOnlyAndBounded(t *testing.T) {
	s := testServer(t, &fakeBackend{}, 0)
	cases := []struct {
		body string
		code int
	}{
		{`{"code":"wrong"}`, http.StatusUnauthorized},
		{`{"code":"test-login-code","alias":"other"}`, http.StatusBadRequest},
		{`{"code":"test-login-code"} {}`, http.StatusBadRequest},
		{`{"code":"` + strings.Repeat("x", maxLoginBody) + `"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, testRequest(http.MethodPost, "/api/login", c.body, nil))
		if w.Code != c.code || len(w.Result().Cookies()) != 0 {
			t.Fatalf("bad login returned %d (expected %d)", w.Code, c.code)
		}
	}
	r := testRequest(http.MethodPost, "/api/login", "code=test-login-code", nil)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatal("login accepted form input")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/login?code=test-login-code", "", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("login accepted a code in the URL")
	}
	for i := 0; i < maxSessions; i++ {
		_, _ = signIn(t, s)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodPost, "/api/login", `{"code":"test-login-code"}`, nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatal("session map was not bounded")
	}
}

func TestReadOnlyRoutesAndSafeErrors(t *testing.T) {
	b := &fakeBackend{list: func(context.Context) ([]consoleapi.Bucket, error) {
		return nil, errors.New("secret-key=PRIVATE https://internal.example/object?signature=SECRET")
	}}
	s := testServer(t, b, 0)
	cookie, _ := signIn(t, s)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, testRequest(method, "/api/buckets", "", cookie))
		if w.Code != http.StatusMethodNotAllowed || b.calls.Load() != 0 {
			t.Fatal("write method reached backend")
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/buckets", "", cookie))
	if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "internal.example") || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatalf("internal error leaked: %d %s", w.Code, w.Body.String())
	}
	b.list = func(context.Context) ([]consoleapi.Bucket, error) {
		return nil, &consoleapi.Error{Status: 403, Code: "access_denied", Message: "Your identity cannot list this bucket."}
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/buckets", "", cookie))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Your identity cannot") {
		t.Fatal("safe backend error was not preserved")
	}
}

func TestObjectQueriesPreserveLiteralKeys(t *testing.T) {
	const prefix = "目录/../百分%2F +?#/"
	const cursor = "opaque+/==cursor"
	b := &fakeBackend{objects: func(_ context.Context, bucket, gotPrefix, gotCursor string, limit int) (consoleapi.Page, error) {
		if bucket != "bucket" || gotPrefix != prefix || gotCursor != cursor || limit != 7 {
			t.Errorf("query was altered: %q %q %q %d", bucket, gotPrefix, gotCursor, limit)
		}
		return consoleapi.Page{Entries: []consoleapi.Entry{{Key: prefix + "file"}}, NextCursor: "next"}, nil
	}}
	s := testServer(t, b, 0)
	cookie, _ := signIn(t, s)
	query := url.Values{"bucket": {"bucket"}, "prefix": {prefix}, "cursor": {cursor}, "limit": {"7"}}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/objects?"+query.Encode(), "", cookie))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "next") {
		t.Fatalf("listing failed: %d %s", w.Code, w.Body.String())
	}
	for _, query := range []string{"bucket=bucket&limit=0", "bucket=bucket&limit=1001", "bucket=bucket&limit=no", "bucket=bucket&bucket=other", "bucket=bucket&prefix=%00", "bucket=bucket&prefix=%xx", "bucket=../unsafe", "bucket=bucket&prefix=" + strings.Repeat("x", 1025)} {
		before := b.calls.Load()
		w = httptest.NewRecorder()
		s.ServeHTTP(w, testRequest(http.MethodGet, "/api/objects?"+query, "", cookie))
		if w.Code != http.StatusBadRequest || b.calls.Load() != before {
			t.Fatalf("invalid query reached storage: %s", query)
		}
	}
}

func TestSessionCancellation(t *testing.T) {
	for _, reason := range []string{"expiry", "logout", "close", "request"} {
		t.Run(reason, func(t *testing.T) {
			started, canceled := make(chan struct{}), make(chan struct{})
			b := &fakeBackend{list: func(ctx context.Context) ([]consoleapi.Bucket, error) {
				close(started)
				<-ctx.Done()
				close(canceled)
				return nil, ctx.Err()
			}}
			ttl := time.Hour
			if reason == "expiry" {
				ttl = 150 * time.Millisecond
			}
			s := testServer(t, b, ttl)
			cookie, response := signIn(t, s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				s.ServeHTTP(httptest.NewRecorder(), testRequest(http.MethodGet, "/api/buckets", "", cookie).WithContext(ctx))
				close(done)
			}()
			waitSignal(t, started)
			switch reason {
			case "logout":
				r := testRequest(http.MethodPost, "/api/logout", "", cookie)
				r.Header.Set("X-CSRF-Token", response.CSRFToken)
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				if w.Code != http.StatusNoContent {
					t.Fatalf("logout failed: %d", w.Code)
				}
			case "close":
				_ = s.Close()
			case "request":
				cancel()
			}
			waitSignal(t, canceled)
			waitSignal(t, done)
			if reason != "request" {
				s.mu.Lock()
				remaining := len(s.sessions)
				s.mu.Unlock()
				if remaining != 0 {
					t.Fatal("invalidated session remained in memory")
				}
				w := httptest.NewRecorder()
				s.ServeHTTP(w, testRequest(http.MethodGet, "/api/session", "", cookie))
				want := http.StatusUnauthorized
				if reason == "close" {
					want = http.StatusServiceUnavailable
				}
				if w.Code != want {
					t.Fatalf("invalid session returned %d, expected %d", w.Code, want)
				}
			}
		})
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not make expected progress")
	}
}

type blockingStream struct {
	first  bool
	closed chan struct{}
	once   sync.Once
}

func (r *blockingStream) Read(p []byte) (int, error) {
	if !r.first {
		r.first = true
		return copy(p, "first chunk"), nil
	}
	<-r.closed
	return 0, errors.New("stream closed")
}

func (r *blockingStream) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
	once    sync.Once
}

func (r *flushRecorder) Flush() {
	r.ResponseRecorder.Flush()
	r.once.Do(func() { close(r.flushed) })
}

func TestDownloadsStreamAndCancelWithoutBuffering(t *testing.T) {
	const key = "目录/../literal%2F +?#/文件\";\r\n.txt"
	stream := &blockingStream{closed: make(chan struct{})}
	b := &fakeBackend{open: func(_ context.Context, bucket, gotKey string) (consoleapi.Object, error) {
		if bucket != "bucket" || gotKey != key {
			t.Errorf("object key was altered: %q", gotKey)
		}
		return consoleapi.Object{Body: stream, Size: -1, ContentType: "text/html"}, nil
	}}
	s := testServer(t, b, 0)
	cookie, _ := signIn(t, s)
	q := url.Values{"bucket": {"bucket"}, "key": {key}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		s.ServeHTTP(w, testRequest(http.MethodGet, "/api/download?"+q.Encode(), "", cookie).WithContext(ctx))
		close(done)
	}()
	waitSignal(t, w.flushed)
	cancel()
	waitSignal(t, stream.closed)
	waitSignal(t, done)
	if w.Code != http.StatusOK || w.Body.String() != "first chunk" || strings.Contains(w.Body.String(), "stream closed") {
		t.Fatalf("stream was buffered or appended an error: %d %s", w.Code, w.Body.String())
	}
	disposition := w.Header().Get("Content-Disposition")
	mediaType, params, err := mime.ParseMediaType(disposition)
	if err != nil || mediaType != "attachment" || params["filename"] != "文件\";.txt" || w.Header().Get("Content-Type") != "application/octet-stream" || strings.ContainsAny(disposition, "\r\n") {
		t.Fatalf("invalid download headers: %v", w.Header())
	}
}

func TestDownloadFailuresAndConcurrencyLimit(t *testing.T) {
	var streams []*blockingStream
	var mu sync.Mutex
	b := &fakeBackend{open: func(_ context.Context, _, key string) (consoleapi.Object, error) {
		if key == "missing" {
			return consoleapi.Object{}, &consoleapi.Error{Status: 404, Code: "not_found", Message: "Object not found."}
		}
		stream := &blockingStream{closed: make(chan struct{})}
		mu.Lock()
		streams = append(streams, stream)
		mu.Unlock()
		return consoleapi.Object{Body: stream, Size: -1}, nil
	}}
	s := testServer(t, b, 0)
	cookie, _ := signIn(t, s)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/download?bucket=bucket&key=missing", "", cookie))
	if w.Code != http.StatusNotFound || w.Header().Get("Content-Disposition") != "" {
		t.Fatal("open failure was presented as a download")
	}
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		w := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
		go func() {
			s.ServeHTTP(w, testRequest(http.MethodGet, "/api/download?bucket=bucket&key=file", "", cookie))
			done <- struct{}{}
		}()
		waitSignal(t, w.flushed)
	}
	before := b.calls.Load()
	w = httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/download?bucket=bucket&key=file", "", cookie))
	if w.Code != http.StatusTooManyRequests || b.calls.Load() != before {
		t.Fatal("download limit did not prevent an upstream call")
	}
	_ = s.Close()
	waitSignal(t, done)
	waitSignal(t, done)
	mu.Lock()
	defer mu.Unlock()
	for _, stream := range streams {
		waitSignal(t, stream.closed)
	}
}

func TestAPIConcurrencyLimit(t *testing.T) {
	started := make(chan struct{}, 8)
	b := &fakeBackend{list: func(ctx context.Context) ([]consoleapi.Bucket, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	s := testServer(t, b, 0)
	cookie, _ := signIn(t, s)
	done := make(chan struct{}, 8)
	for i := 0; i < 8; i++ {
		go func() {
			s.ServeHTTP(httptest.NewRecorder(), testRequest(http.MethodGet, "/api/buckets", "", cookie))
			done <- struct{}{}
		}()
		waitSignal(t, started)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/account", "", cookie))
	if w.Code != http.StatusTooManyRequests || b.calls.Load() != 8 {
		t.Fatal("API concurrency limit did not prevent an upstream call")
	}
	_ = s.Close()
	for i := 0; i < 8; i++ {
		waitSignal(t, done)
	}
}

func TestLoginAttemptLimit(t *testing.T) {
	s := testServer(t, &fakeBackend{}, 0)
	for i := 0; i < maxLoginAttempts; i++ {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, testRequest(http.MethodPost, "/api/login", `{"code":"wrong"}`, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d returned %d", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodPost, "/api/login", `{"code":"test-login-code"}`, nil))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
		t.Fatal("login attempt limit was not enforced")
	}
}

func TestSecurityHeadersAndStaticAssets(t *testing.T) {
	s := testServer(t, &fakeBackend{}, 0)
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, testRequest(http.MethodGet, path, "", nil))
		if w.Code != http.StatusOK || w.Body.Len() == 0 {
			t.Fatalf("asset %s missing: %d", path, w.Code)
		}
		csp := w.Header().Get("Content-Security-Policy")
		if strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "script-src 'self'") || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("missing browser security headers: %v", w.Header())
		}
	}
	for _, path := range []string{"/unknown", "/../app.js", "/api", "/api/unknown"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, testRequest(http.MethodGet, path, "", nil))
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "<!DOCTYPE") {
			t.Fatalf("unexpected fallback for %s", path)
		}
	}
}
