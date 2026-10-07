// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type fakeMutationBackend struct {
	*fakeBackend
	upload func(context.Context, string, string, io.Reader, int64, consoleapi.UploadOptions, func(int64)) (consoleapi.UploadResult, error)
	delete func(context.Context, string, string) error
	scan   func(context.Context, string, string, string, int) (consoleapi.Page, error)
	writes atomic.Int32
	scans  atomic.Int32
}

func (b *fakeMutationBackend) Upload(ctx context.Context, bucket, key string, body io.Reader, size int64, opts consoleapi.UploadOptions, progress func(int64)) (consoleapi.UploadResult, error) {
	b.writes.Add(1)
	if b.upload != nil {
		return b.upload(ctx, bucket, key, body, size, opts, progress)
	}
	n, err := io.Copy(io.Discard, body)
	progress(n)
	return consoleapi.UploadResult{Size: n, ETag: "etag"}, err
}
func (b *fakeMutationBackend) DeleteObject(ctx context.Context, bucket, key string) error {
	b.writes.Add(1)
	if b.delete != nil {
		return b.delete(ctx, bucket, key)
	}
	return nil
}
func (b *fakeMutationBackend) ScanObjects(ctx context.Context, bucket, prefix, cursor string, limit int) (consoleapi.Page, error) {
	b.scans.Add(1)
	if b.scan != nil {
		return b.scan(ctx, bucket, prefix, cursor, limit)
	}
	return consoleapi.Page{}, nil
}

func mutationServer(t *testing.T, b *fakeMutationBackend) *Server {
	t.Helper()
	s, err := New(Config{Backend: b, Alias: "local", BaseURL: testOrigin, LoginCode: "test-login-code", AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Wait(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}
func jobRequest(s *Server, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := testRequest(method, path, body, cookie)
	if method != http.MethodGet {
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if method == http.MethodPut {
		r.Header.Set("Content-Type", "application/octet-stream")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func jobResponse(t *testing.T, w *httptest.ResponseRecorder, status int) consoleapi.Job {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body.String())
	}
	var j consoleapi.Job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	return j
}
func waitJob(t *testing.T, s *Server, id string, cookie *http.Cookie) consoleapi.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j := jobResponse(t, jobRequest(s, http.MethodGet, "/api/jobs/"+id, "", cookie, ""), 200)
		if terminal(j.Status) {
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("task did not finish")
	return consoleapi.Job{}
}

func TestWritesRequireExplicitConfigSessionAndCSRF(t *testing.T) {
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}}
	if _, err := New(Config{Backend: &fakeBackend{}, Alias: "local", BaseURL: testOrigin, LoginCode: "code", AllowWrites: true}); err == nil {
		t.Fatal("accepted read-only backend with writes enabled")
	}
	for _, limit := range []int64{-1, 5<<30 + 1} {
		if _, err := New(Config{Backend: b, Alias: "local", BaseURL: testOrigin, LoginCode: "code", MaxUploadSize: limit}); err == nil {
			t.Fatal("accepted invalid upload limit")
		}
	}
	readOnly, err := New(Config{Backend: b, Alias: "local", BaseURL: testOrigin, LoginCode: "test-login-code"})
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	cookie, reply := signIn(t, readOnly)
	if !reply.ReadOnly || reply.MaxUploadSize != 1<<30 {
		t.Fatalf("bad readonly capability %+v", reply)
	}
	if w := jobRequest(readOnly, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":0}`, cookie, reply.CSRFToken); w.Code != 403 {
		t.Fatalf("default enabled writes: %d", w.Code)
	}
	s := mutationServer(t, b)
	cookie, reply = signIn(t, s)
	if reply.ReadOnly {
		t.Fatal("write capability absent")
	}
	for _, route := range []string{"/api/uploads", "/api/deletions/plan", "/api/jobs/" + strings.Repeat("x", 43) + "/cancel", "/api/deletions/" + strings.Repeat("x", 43) + "/execute", "/api/uploads/" + strings.Repeat("x", 43)} {
		method := http.MethodPost
		if strings.HasPrefix(route, "/api/uploads/") {
			method = http.MethodPut
		}
		if w := jobRequest(s, method, route, "{}", nil, reply.CSRFToken); w.Code != 401 {
			t.Errorf("anonymous %s=%d", route, w.Code)
		}
		if w := jobRequest(s, method, route, "{}", cookie, ""); w.Code != 403 {
			t.Errorf("no CSRF %s=%d", route, w.Code)
		}
		r := testRequest(method, route, "{}", cookie)
		r.Header.Set("X-CSRF-Token", reply.CSRFToken)
		r.Header.Del("Origin")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("no Origin %s=%d", route, w.Code)
		}
	}
	if b.writes.Load() != 0 || b.scans.Load() != 0 {
		t.Fatal("rejected request reached backend")
	}
}

func TestUploadStreamingLiteralKeySizeAndSingleConsumption(t *testing.T) {
	const key = "目录/../literal%2F +?#/file"
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}, upload: func(_ context.Context, bucket, gotKey string, body io.Reader, size int64, opts consoleapi.UploadOptions, progress func(int64)) (consoleapi.UploadResult, error) {
		if bucket != "bucket" || gotKey != key || size != 4 || !opts.Overwrite {
			t.Errorf("upload input altered %q %q %d %+v", bucket, gotKey, size, opts)
		}
		bytes, err := io.ReadAll(body)
		if string(bytes) != "data" {
			t.Errorf("body=%q", bytes)
		}
		progress(2)
		progress(1)
		return consoleapi.UploadResult{Size: int64(len(bytes)), ETag: "saved"}, err
	}}
	s := mutationServer(t, b)
	cookie, reply := signIn(t, s)
	args, _ := json.Marshal(map[string]any{"bucket": "bucket", "key": key, "size": 4, "overwrite": true})
	j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/uploads", string(args), cookie, reply.CSRFToken), 201)
	if j.Status != "waiting" || b.writes.Load() != 0 {
		t.Fatal("upload committed before raw file arrived")
	}
	other, otherReply := signIn(t, s)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		path := "/api/jobs/" + j.ID
		if method == http.MethodPost {
			path += "/cancel"
		}
		if method == http.MethodPut {
			path = "/api/uploads/" + j.ID
		}
		if w := jobRequest(s, method, path, "data", other, otherReply.CSRFToken); w.Code != 404 {
			t.Fatalf("cross-session task visible: %s %d", method, w.Code)
		}
	}
	if w := jobRequest(s, http.MethodPut, "/api/uploads/"+j.ID, "bad", cookie, reply.CSRFToken); w.Code != 400 {
		t.Fatal("wrong size accepted")
	}
	j = jobResponse(t, jobRequest(s, http.MethodPut, "/api/uploads/"+j.ID, "data", cookie, reply.CSRFToken), 200)
	if j.Status != "succeeded" || j.Transferred != 4 || j.ETag != "saved" || j.Completed != 1 {
		t.Fatalf("bad final upload %+v", j)
	}
	if w := jobRequest(s, http.MethodPut, "/api/uploads/"+j.ID, "data", cookie, reply.CSRFToken); w.Code != 409 || b.writes.Load() != 1 {
		t.Fatal("upload task was reused")
	}
}

func TestUploadFailureDoesNotReportCommitOrLeakErrors(t *testing.T) {
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}, upload: func(_ context.Context, _ string, _ string, body io.Reader, _ int64, _ consoleapi.UploadOptions, progress func(int64)) (consoleapi.UploadResult, error) {
		_, _ = io.Copy(io.Discard, body)
		progress(4)
		return consoleapi.UploadResult{}, errors.New("secret-key PRIVATE signed-url SECRET")
	}}
	s := mutationServer(t, b)
	cookie, reply := signIn(t, s)
	j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":4}`, cookie, reply.CSRFToken), 201)
	w := jobRequest(s, http.MethodPut, "/api/uploads/"+j.ID, "data", cookie, reply.CSRFToken)
	j = jobResponse(t, w, 200)
	if j.Status != "failed" || j.Error == nil || j.Completed != 0 || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatalf("bad failure %+v", j)
	}
}

func TestUploadCancellationClosesReaderAndAllowsReads(t *testing.T) {
	for _, action := range []string{"cancel", "logout", "close", "disconnect", "expiry"} {
		t.Run(action, func(t *testing.T) {
			started := make(chan struct{})
			body := &blockingStream{closed: make(chan struct{})}
			b := &fakeMutationBackend{fakeBackend: &fakeBackend{}, upload: func(ctx context.Context, _ string, _ string, reader io.Reader, _ int64, _ consoleapi.UploadOptions, _ func(int64)) (consoleapi.UploadResult, error) {
				close(started)
				_, err := io.Copy(io.Discard, reader)
				if ctx.Err() == nil {
					t.Error("upload canceled without context")
				}
				return consoleapi.UploadResult{}, err
			}}
			s := mutationServer(t, b)
			if action == "expiry" {
				s.ttl = 200 * time.Millisecond
			}
			cookie, reply := signIn(t, s)
			j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":100}`, cookie, reply.CSRFToken), 201)
			r := testRequest(http.MethodPut, "/api/uploads/"+j.ID, "", cookie)
			r.Body = body
			r.ContentLength = 100
			r.Header.Set("Origin", testOrigin)
			r.Header.Set("X-CSRF-Token", reply.CSRFToken)
			r.Header.Set("Content-Type", "application/octet-stream")
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			done := make(chan struct{})
			w := httptest.NewRecorder()
			go func() { s.ServeHTTP(w, r); close(done) }()
			waitSignal(t, started)
			if read := jobRequest(s, http.MethodGet, "/api/buckets", "", cookie, ""); read.Code != 200 {
				t.Fatal("upload blocked listing")
			}
			switch action {
			case "cancel":
				jobResponse(t, jobRequest(s, http.MethodPost, "/api/jobs/"+j.ID+"/cancel", "", cookie, reply.CSRFToken), 200)
			case "logout":
				if out := jobRequest(s, http.MethodPost, "/api/logout", "", cookie, reply.CSRFToken); out.Code != 204 {
					t.Fatal("logout failed")
				}
			case "close":
				_ = s.Close()
			case "disconnect":
				cancel()
			case "expiry":
				// The session timer, rather than this test, cancels the stream.
			}
			waitSignal(t, done)
			waitSignal(t, body.closed)
			final := jobResponse(t, w, 200)
			if final.Status != "canceled" || final.Completed != 0 {
				t.Fatalf("cancel falsely committed %+v", final)
			}
			if len(s.writeSlots) != 0 {
				t.Fatal("write slot leaked")
			}
		})
	}
}

func TestDeletionPlanPaginationConfirmationAndPartialResults(t *testing.T) {
	const prefix = "目录/../literal%2F +?#/"
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}, scan: func(_ context.Context, bucket, gotPrefix, cursor string, limit int) (consoleapi.Page, error) {
		if bucket != "bucket" || gotPrefix != prefix || limit < 1 {
			t.Errorf("scan inputs changed %q %q %d", bucket, gotPrefix, limit)
		}
		if cursor == "" {
			return consoleapi.Page{Entries: []consoleapi.Entry{{Key: prefix + "a"}}, NextCursor: "next"}, nil
		}
		if cursor != "next" {
			t.Error("opaque cursor changed")
		}
		return consoleapi.Page{Entries: []consoleapi.Entry{{Key: prefix + "b"}, {Key: prefix + "c"}}}, nil
	}, delete: func(_ context.Context, _ string, key string) error {
		if key == prefix+"b" {
			return &consoleapi.Error{Status: 403, Code: "access_denied", Message: "Deletion denied."}
		}
		if key == prefix+"c" {
			return errors.New("secret PRIVATE network uncertain")
		}
		return nil
	}}
	s := mutationServer(t, b)
	cookie, reply := signIn(t, s)
	args, _ := json.Marshal(map[string]string{"bucket": "bucket", "prefix": prefix})
	j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/plan", string(args), cookie, reply.CSRFToken), 200)
	if j.Status != "ready" || j.Count != 3 || len(j.ConfirmToken) != 43 || b.writes.Load() != 0 {
		t.Fatalf("invalid plan %+v", j)
	}
	read := jobResponse(t, jobRequest(s, http.MethodGet, "/api/jobs/"+j.ID, "", cookie, ""), 200)
	if read.ConfirmToken != "" {
		t.Fatal("status disclosed confirmation token")
	}
	if w := jobRequest(s, http.MethodPost, "/api/deletions/"+j.ID+"/execute", `{"confirmToken":"wrong"}`, cookie, reply.CSRFToken); w.Code != 403 || b.writes.Load() != 0 {
		t.Fatal("forged confirmation executed")
	}
	confirm, _ := json.Marshal(map[string]string{"confirmToken": j.ConfirmToken})
	jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/"+j.ID+"/execute", string(confirm), cookie, reply.CSRFToken), 202)
	final := waitJob(t, s, j.ID, cookie)
	if final.Status != "partial" || final.Completed != 1 || final.Items[0].Status != "succeeded" || final.Items[1].Status != "failed" || final.Items[2].Status != "unknown" {
		t.Fatalf("partial results=%+v", final)
	}
	if w := jobRequest(s, http.MethodPost, "/api/deletions/"+j.ID+"/execute", string(confirm), cookie, reply.CSRFToken); w.Code != 409 || b.writes.Load() != 3 {
		t.Fatal("confirmation replay deleted again")
	}
	if b.scans.Load() != 2 {
		t.Fatal("execution relisted rather than using fixed snapshot")
	}
}

func TestDeletionPlanningFailureNeverDeletes(t *testing.T) {
	for _, mode := range []string{"denied", "over-limit", "cycle", "outside-prefix", "duplicate", "directory-entry"} {
		t.Run(mode, func(t *testing.T) {
			b := &fakeMutationBackend{fakeBackend: &fakeBackend{}, scan: func(context.Context, string, string, string, int) (consoleapi.Page, error) {
				switch mode {
				case "denied":
					return consoleapi.Page{}, &consoleapi.Error{Status: 403, Code: "access_denied", Message: "List denied."}
				case "over-limit":
					page := consoleapi.Page{}
					for i := 0; i < 1001; i++ {
						page.Entries = append(page.Entries, consoleapi.Entry{Key: fmt.Sprintf("dir/%04d", i)})
					}
					return page, nil
				case "cycle":
					return consoleapi.Page{NextCursor: "same"}, nil
				case "outside-prefix":
					return consoleapi.Page{Entries: []consoleapi.Entry{{Key: "outside"}}}, nil
				case "duplicate":
					return consoleapi.Page{Entries: []consoleapi.Entry{{Key: "dir/a"}, {Key: "dir/a"}}}, nil
				default:
					return consoleapi.Page{Entries: []consoleapi.Entry{{Key: "dir/a", IsPrefix: true}}}, nil
				}
			}}
			s := mutationServer(t, b)
			cookie, reply := signIn(t, s)
			j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/plan", `{"bucket":"bucket","prefix":"dir/"}`, cookie, reply.CSRFToken), 200)
			if j.Status != "failed" || j.ConfirmToken != "" || j.Error == nil || b.writes.Load() != 0 {
				t.Fatalf("unsafe plan %+v", j)
			}
		})
	}
}

func TestDeletionCancellationPreservesCompletedAndPending(t *testing.T) {
	started := make(chan struct{})
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}, delete: func(ctx context.Context, _ string, key string) error {
		if key == "a" {
			return nil
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	s := mutationServer(t, b)
	cookie, reply := signIn(t, s)
	j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/plan", `{"bucket":"bucket","keys":["a","b","c"]}`, cookie, reply.CSRFToken), 200)
	if b.scans.Load() != 0 {
		t.Fatal("explicit keys required listing")
	}
	confirm, _ := json.Marshal(map[string]string{"confirmToken": j.ConfirmToken})
	jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/"+j.ID+"/execute", string(confirm), cookie, reply.CSRFToken), 202)
	waitSignal(t, started)
	jobResponse(t, jobRequest(s, http.MethodPost, "/api/jobs/"+j.ID+"/cancel", "", cookie, reply.CSRFToken), 200)
	final := waitJob(t, s, j.ID, cookie)
	if final.Status != "canceled" || final.Completed != 1 || final.Items[0].Status != "succeeded" || final.Items[1].Status != "unknown" || final.Items[2].Status != "pending" || b.writes.Load() != 2 {
		t.Fatalf("cancel lost partial outcomes %+v", final)
	}
}

func TestTaskCapacityExpiryAndSessionRecovery(t *testing.T) {
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}}
	s := mutationServer(t, b)
	cookie, reply := signIn(t, s)
	var first consoleapi.Job
	for i := 0; i < 16; i++ {
		j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":0}`, cookie, reply.CSRFToken), 201)
		if i == 0 {
			first = j
		}
	}
	if w := jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":0}`, cookie, reply.CSRFToken); w.Code != 429 {
		t.Fatal("per-session task map unbounded")
	}
	w := jobRequest(s, http.MethodGet, "/api/jobs", "", cookie, "")
	var list struct {
		Jobs []consoleapi.Job `json:"jobs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Jobs) != 16 {
		t.Fatal("task collection did not restore session jobs")
	}
	other, _ := signIn(t, s)
	w = jobRequest(s, http.MethodGet, "/api/jobs", "", other, "")
	if strings.TrimSpace(w.Body.String()) != "{\"jobs\":[]}" {
		t.Fatal("other session sees task list")
	}
	s.mu.Lock()
	task := s.jobs[first.ID]
	task.info.Expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.expireJob(task)
	if j := jobResponse(t, jobRequest(s, http.MethodGet, "/api/jobs/"+first.ID, "", cookie, ""), 200); j.Status != "canceled" {
		t.Fatal("waiting task did not expire")
	}
	s.mu.Lock()
	task.info.Expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.expireJob(task)
	if w := jobRequest(s, http.MethodGet, "/api/jobs/"+first.ID, "", cookie, ""); w.Code != 404 {
		t.Fatal("terminal task not cleaned")
	}
	jobResponse(t, jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":0}`, cookie, reply.CSRFToken), 201)
}

func TestDownloadUpstreamIdleClosesStream(t *testing.T) {
	body := &blockingStream{closed: make(chan struct{})}
	s := testServer(t, &fakeBackend{open: func(context.Context, string, string) (consoleapi.Object, error) {
		return consoleapi.Object{Body: body, Size: -1}, nil
	}}, 0)
	s.streamIdle = 20 * time.Millisecond
	cookie, _ := signIn(t, s)
	done := make(chan struct{})
	go func() {
		s.ServeHTTP(httptest.NewRecorder(), testRequest(http.MethodGet, "/api/download?bucket=bucket&key=key", "", cookie))
		close(done)
	}()
	waitSignal(t, done)
	waitSignal(t, body.closed)
}

func TestTaskPressureEvictsTerminalAndPreservesActive(t *testing.T) {
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}}
	s := mutationServer(t, b)
	cookies := make([]*http.Cookie, 5)
	sessions := make([]sessionReply, 5)
	first := make([]consoleapi.Job, 4)
	for user := 0; user < 5; user++ {
		cookies[user], sessions[user] = signIn(t, s)
	}
	for user := 0; user < 4; user++ {
		for i := 0; i < 16; i++ {
			j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":0}`, cookies[user], sessions[user].CSRFToken), 201)
			if i == 0 {
				first[user] = j
			}
		}
	}
	if w := jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":0}`, cookies[4], sessions[4].CSRFToken); w.Code != 429 {
		t.Fatal("global active capacity was bypassed")
	}
	// A successful zero-byte upload becomes reclaimable under its owner's
	// session pressure. The other 15 waiting jobs must survive.
	jobResponse(t, jobRequest(s, http.MethodPut, "/api/uploads/"+first[0].ID, "", cookies[0], sessions[0].CSRFToken), 200)
	jobResponse(t, jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":0}`, cookies[0], sessions[0].CSRFToken), 201)
	if w := jobRequest(s, http.MethodGet, "/api/jobs/"+first[0].ID, "", cookies[0], ""); w.Code != 404 {
		t.Fatal("same-session terminal task was not reclaimed")
	}
	// The fifth session can reclaim a terminal result globally, but cannot
	// reclaim a ready/waiting/running task belonging to any session.
	jobResponse(t, jobRequest(s, http.MethodPost, "/api/jobs/"+first[1].ID+"/cancel", "", cookies[1], sessions[1].CSRFToken), 200)
	jobResponse(t, jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":0}`, cookies[4], sessions[4].CSRFToken), 201)
	if w := jobRequest(s, http.MethodGet, "/api/jobs/"+first[1].ID, "", cookies[1], ""); w.Code != 404 {
		t.Fatal("global terminal task was not reclaimed")
	}
	if j := jobResponse(t, jobRequest(s, http.MethodGet, "/api/jobs/"+first[2].ID, "", cookies[2], ""), 200); j.Status != "waiting" {
		t.Fatal("active task was evicted")
	}
	s.mu.Lock()
	count := len(s.jobs)
	s.mu.Unlock()
	if count != 64 {
		t.Fatalf("job map count=%d", count)
	}
}

func TestWriteConcurrencyDoesNotConsumeConfirmationOnBusy(t *testing.T) {
	started := make(chan struct{}, 2)
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}, upload: func(ctx context.Context, _ string, _ string, _ io.Reader, _ int64, _ consoleapi.UploadOptions, _ func(int64)) (consoleapi.UploadResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return consoleapi.UploadResult{}, ctx.Err()
	}}
	s := mutationServer(t, b)
	cookie, reply := signIn(t, s)
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"bucket","key":"key","size":0}`, cookie, reply.CSRFToken), 201)
		go func() {
			jobRequest(s, http.MethodPut, "/api/uploads/"+j.ID, "", cookie, reply.CSRFToken)
			done <- struct{}{}
		}()
		waitSignal(t, started)
	}
	j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/plan", `{"bucket":"bucket","keys":["key"]}`, cookie, reply.CSRFToken), 200)
	confirm, _ := json.Marshal(map[string]string{"confirmToken": j.ConfirmToken})
	if w := jobRequest(s, http.MethodPost, "/api/deletions/"+j.ID+"/execute", string(confirm), cookie, reply.CSRFToken); w.Code != 429 || b.writes.Load() != 2 {
		t.Fatal("write concurrency limit bypassed")
	}
	if w := jobRequest(s, http.MethodGet, "/api/buckets", "", cookie, ""); w.Code != 200 {
		t.Fatal("writes occupied read slots")
	}
	s.mu.Lock()
	running := []string{}
	for id, j := range s.jobs {
		if j.info.Status == "running" {
			running = append(running, id)
		}
	}
	s.mu.Unlock()
	for _, id := range running {
		jobResponse(t, jobRequest(s, http.MethodPost, "/api/jobs/"+id+"/cancel", "", cookie, reply.CSRFToken), 200)
	}
	waitSignal(t, done)
	waitSignal(t, done)
	jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/"+j.ID+"/execute", string(confirm), cookie, reply.CSRFToken), 202)
	if final := waitJob(t, s, j.ID, cookie); final.Status != "succeeded" || b.writes.Load() != 3 {
		t.Fatalf("busy consumed confirmation token %+v", final)
	}
}

func TestMutationInputLimitsAndEmptySnapshot(t *testing.T) {
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}}
	s := mutationServer(t, b)
	s.maxUploadSize = 4
	cookie, reply := signIn(t, s)
	for _, body := range []string{`{"bucket":"bucket","key":"key","size":5}`, `{"bucket":"bucket","key":"key","size":-1}`, `{"bucket":"bucket","key":"","size":0}`, `{"bucket":"bucket","key":"key","size":0,"alias":"other"}`} {
		if w := jobRequest(s, http.MethodPost, "/api/uploads", body, cookie, reply.CSRFToken); w.Code != 400 {
			t.Fatalf("invalid upload accepted %s: %d", body, w.Code)
		}
	}
	for _, body := range []string{`{"bucket":"bucket","prefix":""}`, `{"bucket":"bucket","prefix":"dir"}`, `{"bucket":"bucket","keys":[]}`, `{"bucket":"bucket","keys":["key","key"]}`, `{"bucket":"bucket","keys":["key"],"prefix":"dir/"}`, `{"bucket":"bucket","keys":["key"],"versionId":"permanent"}`} {
		if w := jobRequest(s, http.MethodPost, "/api/deletions/plan", body, cookie, reply.CSRFToken); w.Code != 400 {
			t.Fatalf("invalid deletion accepted %s: %d", body, w.Code)
		}
	}
	if b.writes.Load() != 0 || b.scans.Load() != 0 {
		t.Fatal("invalid input reached storage")
	}
	j := jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/plan", `{"bucket":"bucket","prefix":"dir/"}`, cookie, reply.CSRFToken), 200)
	if j.Status != "ready" || j.Count != 0 {
		t.Fatalf("empty snapshot %+v", j)
	}
	confirm, _ := json.Marshal(map[string]string{"confirmToken": j.ConfirmToken})
	j = jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/"+j.ID+"/execute", string(confirm), cookie, reply.CSRFToken), 200)
	if j.Status != "succeeded" || b.writes.Load() != 0 {
		t.Fatal("empty plan invoked deletion")
	}
}

func TestPrefixPlanningConcurrencyLeavesReadSlotsAvailable(t *testing.T) {
	started := make(chan struct{}, 2)
	done := make(chan struct{}, 2)
	b := &fakeMutationBackend{fakeBackend: &fakeBackend{}, scan: func(ctx context.Context, _ string, _ string, _ string, _ int) (consoleapi.Page, error) {
		started <- struct{}{}
		<-ctx.Done()
		return consoleapi.Page{}, ctx.Err()
	}}
	s := mutationServer(t, b)
	cookie, reply := signIn(t, s)
	for i := 0; i < 2; i++ {
		go func() {
			jobRequest(s, http.MethodPost, "/api/deletions/plan", `{"bucket":"bucket","prefix":"dir/"}`, cookie, reply.CSRFToken)
			done <- struct{}{}
		}()
		waitSignal(t, started)
	}
	if w := jobRequest(s, http.MethodPost, "/api/deletions/plan", `{"bucket":"bucket","prefix":"dir/"}`, cookie, reply.CSRFToken); w.Code != 429 || b.scans.Load() != 2 {
		t.Fatal("planning concurrency unbounded")
	}
	if w := jobRequest(s, http.MethodGet, "/api/buckets", "", cookie, ""); w.Code != 200 {
		t.Fatal("planning consumed listing slots")
	}
	_ = s.Close()
	waitSignal(t, done)
	waitSignal(t, done)
	if len(s.planSlots) != 0 || b.writes.Load() != 0 {
		t.Fatal("planning leaked a slot or deleted objects")
	}
}
