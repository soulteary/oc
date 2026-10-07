// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

// The listener uses real HTTP connections but stalls one selected socket write.
// This makes otherwise tiny empty headers/final chunks deterministically block.
type downloadGateListener struct {
	net.Listener
	mode    string
	entered chan time.Time
	mu      sync.Mutex
	blocked bool
	accepts int
}

func (l *downloadGateListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.accepts++
	l.mu.Unlock()
	return &downloadGateConn{Conn: conn, owner: l, changed: make(chan struct{}, 1), closed: make(chan struct{})}, nil
}

func (l *downloadGateListener) selectWrite(data []byte) bool {
	match := l.mode == "headers" && bytes.Contains(data, []byte("Content-Disposition: attachment")) ||
		l.mode == "final-chunk" && bytes.Equal(data, []byte("0\r\n\r\n"))
	l.mu.Lock()
	defer l.mu.Unlock()
	if !match || l.blocked {
		return false
	}
	l.blocked = true
	return true
}

type downloadGateConn struct {
	net.Conn
	owner    *downloadGateListener
	mu       sync.Mutex
	deadline time.Time
	changed  chan struct{}
	closed   chan struct{}
	once     sync.Once
}

func (c *downloadGateConn) SetWriteDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.deadline = deadline
	c.mu.Unlock()
	select {
	case c.changed <- struct{}{}:
	default:
	}
	return c.Conn.SetWriteDeadline(deadline)
}

func (c *downloadGateConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func (c *downloadGateConn) Write(data []byte) (int, error) {
	if !c.owner.selectWrite(data) {
		return c.Conn.Write(data)
	}
	c.mu.Lock()
	installed := c.deadline
	c.mu.Unlock()
	c.owner.entered <- installed
	if c.owner.mode == "final-chunk" && installed.After(time.Now()) {
		// Shorten an existing deadline for the test; never invent a deadline
		// when finishRequest has received an unprotected connection.
		_ = c.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
	}
	for {
		c.mu.Lock()
		deadline := c.deadline
		c.mu.Unlock()
		var timeout <-chan time.Time
		var timer *time.Timer
		if !deadline.IsZero() {
			if !deadline.After(time.Now()) {
				return 0, os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(time.Until(deadline))
			timeout = timer.C
		}
		select {
		case <-c.closed:
			if timer != nil {
				timer.Stop()
			}
			return 0, net.ErrClosed
		case <-timeout:
			return 0, os.ErrDeadlineExceeded
		case <-c.changed:
			if timer != nil {
				timer.Stop()
			}
		}
	}
}

func gatedDownloadConsole(t *testing.T, backend consoleapi.Backend, mode string) (*Server, *httptest.Server, *downloadGateListener, <-chan struct{}) {
	t.Helper()
	var handler *Server
	finished := make(chan struct{})
	native := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/download" {
			defer close(finished)
		}
		handler.ServeHTTP(w, r)
	}))
	gate := &downloadGateListener{Listener: native.Listener, mode: mode, entered: make(chan time.Time, 1)}
	native.Listener = gate
	var err error
	handler, err = New(Config{Backend: backend, Alias: "local", BaseURL: "http://" + native.Listener.Addr().String(), LoginCode: "test-login-code"})
	if err != nil {
		t.Fatal(err)
	}
	native.Start()
	t.Cleanup(func() { _ = handler.Close(); native.CloseClientConnections(); native.Close() })
	return handler, native, gate, finished
}

func TestEmptyDownloadHeadersFlushUnderCancelableDeadline(t *testing.T) {
	backend := &fakeBackend{open: func(context.Context, string, string) (consoleapi.Object, error) {
		return consoleapi.Object{Body: io.NopCloser(strings.NewReader("")), Size: 0}, nil
	}}
	handler, native, gate, finished := gatedDownloadConsole(t, backend, "headers")
	login, _ := nativeJSON(t, native, "/api/login", map[string]string{"code": "test-login-code"}, nil, "")
	request, _ := http.NewRequest(http.MethodGet, native.URL+"/api/download?bucket=bucket&key=empty", nil)
	request.AddCookie(login.Cookies()[0])
	result := make(chan error, 1)
	go func() {
		response, err := native.Client().Do(request)
		if response != nil {
			response.Body.Close()
		}
		result <- err
	}()
	select {
	case deadline := <-gate.entered:
		if !deadline.After(time.Now()) {
			t.Fatal("empty download headers have no active write deadline")
		}
	case <-time.After(time.Second):
		t.Fatal("empty download never flushed its headers")
	}
	_ = handler.Close()
	waitSignal(t, finished)
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled blocked header flush appeared successful")
		}
	case <-time.After(time.Second):
		t.Fatal("closing the console did not interrupt empty download flush")
	}
}

func TestUnknownDownloadFinalChunkRetainsWriteDeadline(t *testing.T) {
	backend := &fakeBackend{open: func(context.Context, string, string) (consoleapi.Object, error) {
		return consoleapi.Object{Body: io.NopCloser(strings.NewReader("payload")), Size: -1}, nil
	}}
	_, native, gate, finished := gatedDownloadConsole(t, backend, "final-chunk")
	login, _ := nativeJSON(t, native, "/api/login", map[string]string{"code": "test-login-code"}, nil, "")
	request, _ := http.NewRequest(http.MethodGet, native.URL+"/api/download?bucket=bucket&key=unknown", nil)
	request.AddCookie(login.Cookies()[0])
	response, err := native.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	waitSignal(t, finished)
	select {
	case deadline := <-gate.entered:
		if !deadline.After(time.Now()) {
			t.Fatal("finishRequest received a chunked download without a write deadline")
		}
	case <-time.After(time.Second):
		t.Fatal("chunked response did not attempt its terminating chunk")
	}
	result := make(chan error, 1)
	go func() {
		data, readErr := io.ReadAll(response.Body)
		if string(data) != "payload" {
			t.Errorf("download body=%q", data)
		}
		result <- readErr
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("blocked final chunk appeared to complete successfully")
		}
	case <-time.After(time.Second):
		t.Fatal("final chunk write ignored its deadline")
	}
}

func TestEmptyAndUnknownDownloadsCompleteAndAllowConnectionReuse(t *testing.T) {
	for _, size := range []int64{0, -1} {
		t.Run(map[int64]string{0: "empty", -1: "unknown"}[size], func(t *testing.T) {
			payload := ""
			if size < 0 {
				payload = "payload"
			}
			backend := &fakeBackend{open: func(context.Context, string, string) (consoleapi.Object, error) {
				return consoleapi.Object{Body: io.NopCloser(strings.NewReader(payload)), Size: size}, nil
			}}
			_, native, listener, _ := gatedDownloadConsole(t, backend, "")
			login, _ := nativeJSON(t, native, "/api/login", map[string]string{"code": "test-login-code"}, nil, "")
			for _, path := range []string{"/api/download?bucket=bucket&key=key", "/api/session"} {
				request, _ := http.NewRequest(http.MethodGet, native.URL+path, nil)
				request.AddCookie(login.Cookies()[0])
				response, err := native.Client().Do(request)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || response.StatusCode != 200 {
					t.Fatalf("response status=%d body=%q error=%v", response.StatusCode, data, err)
				}
				if strings.HasPrefix(path, "/api/download") && string(data) != payload {
					t.Fatalf("download=%q", data)
				}
			}
			listener.mu.Lock()
			connections := listener.accepts
			listener.mu.Unlock()
			if connections != 1 {
				t.Fatalf("download prevented HTTP connection reuse: %d connections", connections)
			}
		})
	}
}
