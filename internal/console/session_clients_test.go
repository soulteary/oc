// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

func ownedClientServer(t *testing.T, backend consoleapi.Backend, factory func(context.Context) (consoleapi.Backend, func(), error), writes bool) *Server {
	t.Helper()
	s, err := New(Config{Backend: backend, BackendFactory: factory, Alias: "local", BaseURL: testOrigin, LoginCode: "test-login-code", AllowWrites: writes, ArchiveDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drainOwnedClients(t, s) })
	return s
}

func drainOwnedClients(t *testing.T, s *Server) {
	t.Helper()
	_ = s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Wait(ctx); err != nil {
		t.Error(err)
	}
}

func awaitClientSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for client lifecycle")
	}
}

func revokeClientSession(s *Server, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	return jobRequest(s, http.MethodPost, "/api/logout", "", cookie, csrf)
}

func TestFactoryCreatesIndependentClientsAndLogoutClosesOnlyItsOwn(t *testing.T) {
	var created, closedFirst, closedSecond atomic.Int32
	first := &fakeBackend{list: func(context.Context) ([]consoleapi.Bucket, error) {
		return []consoleapi.Bucket{{Name: "first-session"}}, nil
	}}
	second := &fakeBackend{list: func(context.Context) ([]consoleapi.Bucket, error) {
		return []consoleapi.Bucket{{Name: "second-session"}}, nil
	}}
	s := ownedClientServer(t, &fakeBackend{}, func(context.Context) (consoleapi.Backend, func(), error) {
		if created.Add(1) == 1 {
			return first, func() { closedFirst.Add(1) }, nil
		}
		return second, func() { closedSecond.Add(1) }, nil
	}, false)
	firstCookie, firstReply := signIn(t, s)
	secondCookie, _ := signIn(t, s)
	for _, tc := range []struct {
		cookie *http.Cookie
		want   string
	}{{firstCookie, "first-session"}, {secondCookie, "second-session"}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, testRequest(http.MethodGet, "/api/buckets", "", tc.cookie))
		if w.Code != 200 || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("connection was shared: %d %s", w.Code, w.Body.String())
		}
	}
	if w := revokeClientSession(s, firstCookie, firstReply.CSRFToken); w.Code != 204 {
		t.Fatal(w.Code)
	}
	// Observe disposal through a bounded eventual check; callbacks run off the handler lock.
	deadline := time.Now().Add(time.Second)
	for closedFirst.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if closedFirst.Load() != 1 || closedSecond.Load() != 0 {
		t.Fatal("logout closed the wrong client")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/buckets", "", secondCookie))
	if w.Code != 200 {
		t.Fatal("another session was revoked")
	}
	drainOwnedClients(t, s)
	if closedFirst.Load() != 1 || closedSecond.Load() != 1 {
		t.Fatal("client cleanup was missed or repeated")
	}
}

func TestLogoutWaitsForRequestCleanupBeforeClosingClient(t *testing.T) {
	canceled, finish, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	started := make(chan struct{})
	backend := &fakeBackend{list: func(ctx context.Context) ([]consoleapi.Bucket, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-finish
		return nil, ctx.Err()
	}}
	s := ownedClientServer(t, backend, func(context.Context) (consoleapi.Backend, func(), error) {
		return backend, func() { close(closed) }, nil
	}, false)
	cookie, reply := signIn(t, s)
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		s.ServeHTTP(httptest.NewRecorder(), testRequest(http.MethodGet, "/api/buckets", "", cookie))
	}()
	awaitClientSignal(t, started)
	revokeClientSession(s, cookie, reply.CSRFToken)
	awaitClientSignal(t, canceled)
	select {
	case <-closed:
		t.Fatal("transport closed before canceled request cleanup")
	default:
	}
	close(finish)
	awaitClientSignal(t, requestDone)
	awaitClientSignal(t, closed)
}

func TestLogoutWaitsForBackgroundDeletionCleanup(t *testing.T) {
	started, canceled, finish, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	backend := &fakeMutationBackend{fakeBackend: &fakeBackend{}, delete: func(ctx context.Context, _, _ string) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-finish
		return ctx.Err()
	}}
	s := ownedClientServer(t, backend, func(context.Context) (consoleapi.Backend, func(), error) {
		return backend, func() { close(closed) }, nil
	}, true)
	cookie, reply := signIn(t, s)
	planned := jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/plan", `{"bucket":"bucket","keys":["key"]}`, cookie, reply.CSRFToken), 200)
	confirmation, _ := json.Marshal(map[string]string{"confirmToken": planned.ConfirmToken})
	jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/"+planned.ID+"/execute", string(confirmation), cookie, reply.CSRFToken), 202)
	awaitClientSignal(t, started)
	revokeClientSession(s, cookie, reply.CSRFToken)
	awaitClientSignal(t, canceled)
	select {
	case <-closed:
		t.Fatal("transport closed before background mutation cleanup")
	default:
	}
	close(finish)
	awaitClientSignal(t, closed)
}

func TestLogoutWaitsForBackgroundArchiveCleanup(t *testing.T) {
	started, canceled, finish, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	backend := &archiveBackend{scan: func(ctx context.Context, _, _, _ string, _ int) (consoleapi.Page, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-finish
		return consoleapi.Page{}, ctx.Err()
	}}
	s := ownedClientServer(t, backend, func(context.Context) (consoleapi.Backend, func(), error) {
		return backend, func() { close(closed) }, nil
	}, false)
	cookie, reply := signIn(t, s)
	startArchive(t, s, cookie, reply.CSRFToken, `{"bucket":"bucket","prefix":"dir/"}`)
	awaitClientSignal(t, started)
	revokeClientSession(s, cookie, reply.CSRFToken)
	awaitClientSignal(t, canceled)
	select {
	case <-closed:
		t.Fatal("transport closed before archive cleanup")
	default:
	}
	close(finish)
	awaitClientSignal(t, closed)
}

func TestCloseDrainsPendingLoginClientWithoutPublishingSession(t *testing.T) {
	started, canceled, finish, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	s := ownedClientServer(t, &fakeBackend{}, func(ctx context.Context) (consoleapi.Backend, func(), error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-finish
		return &fakeBackend{}, func() { close(closed) }, nil
	}, false)
	response := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		s.ServeHTTP(response, testRequest(http.MethodPost, "/api/login", `{"code":"test-login-code"}`, nil))
	}()
	awaitClientSignal(t, started)
	_ = s.Close()
	awaitClientSignal(t, canceled)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pending factory wasn't tracked: %v", err)
	}
	close(finish)
	awaitClientSignal(t, requestDone)
	awaitClientSignal(t, closed)
	if len(response.Result().Cookies()) != 0 || len(s.sessions) != 0 {
		t.Fatal("shutdown published a session")
	}
}

func TestFactoryFailureCleansPartialClientAndHidesSecrets(t *testing.T) {
	var attempts, closed atomic.Int32
	s := ownedClientServer(t, &fakeBackend{}, func(context.Context) (consoleapi.Backend, func(), error) {
		attempts.Add(1)
		return &fakeBackend{}, func() { closed.Add(1) }, errors.New("secret-access-key")
	}, false)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodPost, "/api/login", `{"code":"wrong"}`, nil))
	if w.Code != 401 || attempts.Load() != 0 {
		t.Fatal("invalid code allocated a client")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodPost, "/api/login", `{"code":"test-login-code"}`, nil))
	if w.Code != 503 || closed.Load() != 1 || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "secret-access-key") {
		t.Fatal("failed factory leaked a client, session or secret")
	}
}

func TestUploadCancellationKeepsClientAliveThroughAbortCleanup(t *testing.T) {
	started, canceled, finish, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	backend := &fakeMutationBackend{fakeBackend: &fakeBackend{}, upload: func(ctx context.Context, _, _ string, _ io.Reader, _ int64, _ consoleapi.UploadOptions, _ func(int64)) (consoleapi.UploadResult, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		// Model the client's multipart-abort cleanup, which uses the transport after
		// the operation's context has been canceled.
		<-finish
		select {
		case <-closed:
			t.Error("client closed during multipart cleanup")
		default:
		}
		return consoleapi.UploadResult{}, ctx.Err()
	}}
	s := ownedClientServer(t, backend, func(context.Context) (consoleapi.Backend, func(), error) {
		return backend, func() { close(closed) }, nil
	}, true)
	cookie, reply := signIn(t, s)
	upload := jobResponse(t, jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"object","size":4}`, cookie, reply.CSRFToken), 201)
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		jobRequest(s, http.MethodPut, "/api/uploads/"+upload.ID, "data", cookie, reply.CSRFToken)
	}()
	awaitClientSignal(t, started)
	revokeClientSession(s, cookie, reply.CSRFToken)
	awaitClientSignal(t, canceled)
	select {
	case <-closed:
		t.Fatal("upload transport closed before abort cleanup")
	default:
	}
	close(finish)
	awaitClientSignal(t, requestDone)
	awaitClientSignal(t, closed)
}

func TestSessionExpiryDisposesOwnedClient(t *testing.T) {
	closed := make(chan struct{})
	s := ownedClientServer(t, &fakeBackend{}, func(context.Context) (consoleapi.Backend, func(), error) {
		return &fakeBackend{}, func() { close(closed) }, nil
	}, false)
	s.ttl = 50 * time.Millisecond
	cookie, _ := signIn(t, s)
	awaitClientSignal(t, closed)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/buckets", "", cookie))
	if w.Code != 401 {
		t.Fatal("expired session accessed disposed client")
	}
}

func TestSessionLimitClosesRejectedOwnedClient(t *testing.T) {
	var created, closed atomic.Int32
	s := ownedClientServer(t, &fakeBackend{}, func(context.Context) (consoleapi.Backend, func(), error) {
		created.Add(1)
		return &fakeBackend{}, func() { closed.Add(1) }, nil
	}, false)
	for i := 0; i < maxSessions; i++ {
		signIn(t, s)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodPost, "/api/login", `{"code":"test-login-code"}`, nil))
	if w.Code != 429 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("session limit bypassed: %d", w.Code)
	}
	drainOwnedClients(t, s)
	if created.Load() != maxSessions+1 || closed.Load() != created.Load() {
		t.Fatal("rejected login leaked its client")
	}
}

func TestBorrowedBackendDoesNotAcquireFactoryOwnership(t *testing.T) {
	s := testServer(t, &fakeBackend{}, 0)
	cookie, _ := signIn(t, s)
	sess, _ := s.authenticate(testRequest(http.MethodGet, "/api/session", "", cookie))
	if sess.runtime.lifetime != nil {
		t.Fatal("borrowed client became session owned")
	}
	drainOwnedClients(t, s)
}

func TestFactoryRejectsWriteCapabilityMismatchAndCleansClient(t *testing.T) {
	var closed atomic.Int32
	startup := &fakeMutationBackend{fakeBackend: &fakeBackend{}}
	s := ownedClientServer(t, startup, func(context.Context) (consoleapi.Backend, func(), error) {
		return &fakeBackend{}, func() { closed.Add(1) }, nil
	}, true)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodPost, "/api/login", `{"code":"test-login-code"}`, nil))
	if w.Code != 503 || closed.Load() != 1 || len(w.Result().Cookies()) != 0 {
		t.Fatal("incomplete writable client was published or leaked")
	}
}
