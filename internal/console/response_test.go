// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type blockingJSONFlush struct {
	*httptest.ResponseRecorder
	started  chan struct{}
	released chan struct{}
	once     sync.Once
	mu       sync.Mutex
	deadline time.Time
}

func (w *blockingJSONFlush) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	w.deadline = deadline
	w.mu.Unlock()
	if !deadline.IsZero() && !deadline.After(time.Now()) {
		w.once.Do(func() { close(w.released) })
	}
	return nil
}
func (w *blockingJSONFlush) FlushError() error {
	w.mu.Lock()
	valid := w.deadline.After(time.Now())
	w.mu.Unlock()
	if !valid {
		return io.ErrClosedPipe
	}
	close(w.started)
	<-w.released
	return io.ErrClosedPipe
}

func TestJSONErrorFlushNeverHoldsServerMutex(t *testing.T) {
	for _, scenario := range []string{"session-capacity", "upload-busy", "delete-busy"} {
		t.Run(scenario, func(t *testing.T) {
			s := mutationServer(t, &fakeMutationBackend{fakeBackend: &fakeBackend{}})
			cookie, reply := signIn(t, s)
			path := "/api/uploads"
			method := http.MethodPost
			body := `{"bucket":"bucket","key":"key","size":0}`
			if scenario == "session-capacity" {
				for i := 0; i < 16; i++ {
					jobResponse(t, jobRequest(s, method, path, body, cookie, reply.CSRFToken), 201)
				}
			} else {
				if scenario == "upload-busy" {
					j := jobResponse(t, jobRequest(s, method, path, body, cookie, reply.CSRFToken), 201)
					path += "/" + j.ID
					method = http.MethodPut
					body = ""
				} else {
					j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/plan", `{"bucket":"bucket","keys":["key"]}`, cookie, reply.CSRFToken), 200)
					path = "/api/deletions/" + j.ID + "/execute"
					data, _ := json.Marshal(map[string]string{"confirmToken": j.ConfirmToken})
					body = string(data)
				}
				s.writeSlots <- struct{}{}
				s.writeSlots <- struct{}{}
				defer func() { <-s.writeSlots; <-s.writeSlots }()
			}
			r := testRequest(method, path, body, cookie)
			r.Header.Set("Origin", testOrigin)
			r.Header.Set("X-CSRF-Token", reply.CSRFToken)
			if method == http.MethodPut {
				r.Header.Set("Content-Type", "application/octet-stream")
			}
			w := &blockingJSONFlush{ResponseRecorder: httptest.NewRecorder(), started: make(chan struct{}), released: make(chan struct{})}
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() {
					if recovered := recover(); recovered != nil && recovered != http.ErrAbortHandler {
						t.Errorf("unexpected panic %v", recovered)
					}
				}()
				s.ServeHTTP(w, r)
			}()
			waitSignal(t, w.started)
			closed := make(chan struct{})
			go func() { _ = s.Close(); close(closed) }()
			waitSignal(t, closed)
			waitSignal(t, done)
			if w.Code != 429 {
				t.Fatalf("error status=%d", w.Code)
			}
		})
	}
}

func TestSlowJSONSocketFlushIsInterruptedByClose(t *testing.T) {
	for _, size := range []int{16, 64 << 10} {
		t.Run(map[int]string{16: "buffered-flush", 64 << 10: "large-write"}[size], func(t *testing.T) {
			var handler *Server
			finished := make(chan struct{})
			backend := &fakeBackend{account: func(context.Context) (consoleapi.Account, error) {
				return consoleapi.Account{Buckets: []consoleapi.AccountBucket{{Name: strings.Repeat("x", size)}}}, nil
			}}
			native := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/account" {
					defer close(finished)
				}
				handler.ServeHTTP(w, r)
			}))
			gate := &responseGateListener{Listener: native.Listener, mode: "json-body", entered: make(chan time.Time, 1)}
			native.Listener = gate
			var err error
			handler, err = New(Config{Backend: backend, Alias: "local", BaseURL: "http://" + native.Listener.Addr().String(), LoginCode: "test-login-code"})
			if err != nil {
				t.Fatal(err)
			}
			native.Start()
			t.Cleanup(func() { _ = handler.Close(); native.CloseClientConnections(); native.Close() })
			login, _ := nativeJSON(t, native, "/api/login", map[string]string{"code": "test-login-code"}, nil, "")
			connection, err := net.Dial("tcp", native.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
			cookie := login.Cookies()[0]
			_, err = io.WriteString(connection, "GET /api/account HTTP/1.1\r\nHost: "+native.Listener.Addr().String()+"\r\nCookie: "+cookie.Name+"="+cookie.Value+"\r\n\r\n")
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodGet})
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK || response.ContentLength < int64(size) || len(response.TransferEncoding) != 0 {
				t.Fatal("JSON response did not use known content length")
			}
			// Wait for the body write itself instead of assuming that the operating
			// system's socket buffers cannot absorb a fixed-size JSON response.
			select {
			case deadline := <-gate.entered:
				if !deadline.After(time.Now()) {
					t.Fatal("JSON body has no active write deadline")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("JSON body write never reached the socket gate")
			}
			select {
			case <-finished:
				t.Fatal("could not establish a blocked JSON socket write")
			default:
			}
			_ = handler.Close()
			waitSignal(t, finished)
			if _, err := io.ReadAll(response.Body); err == nil {
				t.Fatal("canceled partial JSON looked complete")
			}
		})
	}
}

func TestEarlyRejectedUploadDoesNotDrainSlowSocketBody(t *testing.T) {
	native := nativeConsole(t, &fakeMutationBackend{fakeBackend: &fakeBackend{}}, true, func(string) {})
	login, data := nativeJSON(t, native, "/api/login", map[string]string{"code": "test-login-code"}, nil, "")
	var sess sessionReply
	if err := json.Unmarshal(data, &sess); err != nil {
		t.Fatal(err)
	}
	response, data := nativeJSON(t, native, "/api/uploads", map[string]any{"bucket": "bucket", "key": "key", "size": 100}, login.Cookies()[0], sess.CSRFToken)
	if response.StatusCode != 201 {
		t.Fatal("create failed")
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
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	cookie := login.Cookies()[0]
	_, err = io.WriteString(connection, "PUT /api/uploads/"+j.ID+" HTTP/1.1\r\nHost: "+native.Listener.Addr().String()+"\r\nOrigin: "+native.URL+"\r\nCookie: "+cookie.Name+"="+cookie.Value+"\r\nX-CSRF-Token: "+sess.CSRFToken+"\r\nContent-Type: application/octet-stream\r\nContent-Length: 99\r\n\r\nx")
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPut})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatalf("bad length status=%d", response.StatusCode)
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal("early upload rejection did not finish while body was stalled", err)
	}
}

func TestUploadFinalAcknowledgmentIsFlushedWithKnownLength(t *testing.T) {
	native := nativeConsole(t, &fakeMutationBackend{fakeBackend: &fakeBackend{}}, true, func(string) {})
	login, data := nativeJSON(t, native, "/api/login", map[string]string{"code": "test-login-code"}, nil, "")
	var sess sessionReply
	if err := json.Unmarshal(data, &sess); err != nil {
		t.Fatal(err)
	}
	cookie := login.Cookies()[0]
	for _, file := range []string{"", "data"} {
		_, data = nativeJSON(t, native, "/api/uploads", map[string]any{"bucket": "bucket", "key": "key", "size": len(file)}, cookie, sess.CSRFToken)
		var j consoleapi.Job
		if err := json.Unmarshal(data, &j); err != nil {
			t.Fatal(err)
		}
		r, err := http.NewRequest(http.MethodPut, native.URL+"/api/uploads/"+j.ID, strings.NewReader(file))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", native.URL)
		r.Header.Set("X-CSRF-Token", sess.CSRFToken)
		r.Header.Set("Content-Type", "application/octet-stream")
		r.AddCookie(cookie)
		response, err := native.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		data, err = io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || response.ContentLength != int64(len(data)) || len(response.TransferEncoding) != 0 {
			t.Fatalf("invalid upload acknowledgment headers %+v", response)
		}
		if err := json.Unmarshal(data, &j); err != nil {
			t.Fatal(err)
		}
		if j.Status != "succeeded" || j.Size != int64(len(file)) {
			t.Fatalf("bad acknowledgment %+v", j)
		}
	}
	// A cleared deadline must also permit subsequent use of the connection.
	r, err := http.NewRequest(http.MethodGet, native.URL+"/api/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(cookie)
	response, err := native.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("follow-up request failed")
	}
}
