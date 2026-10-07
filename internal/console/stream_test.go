// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

func nativeConsole(t *testing.T, backend consoleapi.Backend, writes bool, observe func(string)) *httptest.Server {
	t.Helper()
	var handler *Server
	native := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer observe(r.URL.Path); handler.ServeHTTP(w, r) }))
	var err error
	handler, err = New(Config{Backend: backend, Alias: "local", BaseURL: "http://" + native.Listener.Addr().String(), LoginCode: "test-login-code", AllowWrites: writes})
	if err != nil {
		t.Fatal(err)
	}
	native.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer observe(r.URL.Path); handler.ServeHTTP(w, r) })
	native.Start()
	t.Cleanup(func() { _ = handler.Close(); native.Close() })
	return native
}

func nativeJSON(t *testing.T, native *httptest.Server, path string, value any, cookie *http.Cookie, csrf string) (*http.Response, []byte) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest(http.MethodPost, native.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", native.URL)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	response, err := native.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, data
}

func TestSlowUploadSocketCancellationWithoutWholeRequestTimeout(t *testing.T) {
	started, finished := make(chan struct{}), make(chan struct{})
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}, upload: func(ctx context.Context, _ string, _ string, body io.Reader, _ int64, _ consoleapi.UploadOptions, _ func(int64)) (consoleapi.UploadResult, error) {
		close(started)
		_, err := io.Copy(io.Discard, body)
		if ctx.Err() == nil {
			t.Error("socket read canceled without task context")
		}
		return consoleapi.UploadResult{}, err
	}}
	native := nativeConsole(t, b, true, func(path string) {
		if strings.HasPrefix(path, "/api/uploads/") {
			close(finished)
		}
	})
	login, data := nativeJSON(t, native, "/api/login", map[string]string{"code": "test-login-code"}, nil, "")
	if login.StatusCode != 200 || len(login.Cookies()) != 1 {
		t.Fatal("login failed")
	}
	cookie := login.Cookies()[0]
	var sess sessionReply
	if err := json.Unmarshal(data, &sess); err != nil {
		t.Fatal(err)
	}
	response, data := nativeJSON(t, native, "/api/uploads", map[string]any{"bucket": "bucket", "key": "key", "size": 100}, cookie, sess.CSRFToken)
	if response.StatusCode != 201 {
		t.Fatalf("create upload %s", data)
	}
	var j consoleapi.Job
	if err := json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	connection, err := net.Dial("tcp", native.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, err = io.WriteString(connection, "PUT /api/uploads/"+j.ID+" HTTP/1.1\r\nHost: "+native.Listener.Addr().String()+"\r\nOrigin: "+native.URL+"\r\nCookie: "+cookie.Name+"="+cookie.Value+"\r\nX-CSRF-Token: "+sess.CSRFToken+"\r\nContent-Type: application/octet-stream\r\nContent-Length: 100\r\n\r\nfirst")
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, started)
	response, _ = nativeJSON(t, native, "/api/jobs/"+j.ID+"/cancel", struct{}{}, cookie, sess.CSRFToken)
	if response.StatusCode != 200 {
		t.Fatal("cancel rejected")
	}
	waitSignal(t, finished)
	request, err := http.NewRequest(http.MethodGet, native.URL+"/api/jobs/"+j.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	status, err := native.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer status.Body.Close()
	if err := json.NewDecoder(status.Body).Decode(&j); err != nil {
		t.Fatal(err)
	}
	if j.Status != "canceled" || j.Completed != 0 {
		t.Fatalf("canceled socket upload falsely committed %+v", j)
	}
}

func TestCloseInterruptsUnauthenticatedSlowJSON(t *testing.T) {
	var handler *Server
	finished := make(chan struct{})
	native := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(finished); handler.ServeHTTP(w, r) }))
	var err error
	handler, err = New(Config{Backend: &fakeBackend{}, Alias: "local", BaseURL: "http://" + native.Listener.Addr().String(), LoginCode: "code"})
	if err != nil {
		t.Fatal(err)
	}
	native.Start()
	defer native.Close()
	defer handler.Close()
	connection, err := net.Dial("tcp", native.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, err = io.WriteString(connection, "POST /api/login HTTP/1.1\r\nHost: "+native.Listener.Addr().String()+"\r\nOrigin: "+native.URL+"\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"code\":\"")
	if err != nil {
		t.Fatal(err)
	}
	// Observe decoder entry rather than waiting for the ten-second JSON limit.
	deadline := time.Now().Add(time.Second)
	for {
		handler.mu.Lock()
		started := handler.loginCount > 0
		handler.mu.Unlock()
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("login not started")
		}
		time.Sleep(time.Millisecond)
	}
	_ = handler.Close()
	waitSignal(t, finished)
}

func TestChunkedDownloadReadFailureIsNotSuccessfulEOF(t *testing.T) {
	body := &blockingStream{closed: make(chan struct{})}
	var handler *Server
	native := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	var err error
	handler, err = New(Config{Backend: &fakeBackend{open: func(context.Context, string, string) (consoleapi.Object, error) {
		return consoleapi.Object{Body: body, Size: -1}, nil
	}}, Alias: "local", BaseURL: "http://" + native.Listener.Addr().String(), LoginCode: "test-login-code"})
	if err != nil {
		t.Fatal(err)
	}
	handler.streamIdle = 20 * time.Millisecond
	native.Start()
	defer native.Close()
	defer handler.Close()
	login, _ := nativeJSON(t, native, "/api/login", map[string]string{"code": "test-login-code"}, nil, "")
	request, err := http.NewRequest(http.MethodGet, native.URL+"/api/download?bucket=bucket&key=key", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(login.Cookies()[0])
	response, err := native.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if string(data) != "first chunk" || err == nil {
		t.Fatalf("truncated chunked download appeared complete: %q %v", data, err)
	}
}
