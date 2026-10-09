// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type archiveBackend struct {
	fakeBackend
	stat func(context.Context, consoleapi.ObjectRef) (consoleapi.ObjectInfo, error)
	read func(context.Context, consoleapi.ObjectRef, string) (consoleapi.Object, error)
	scan func(context.Context, string, string, string, int) (consoleapi.Page, error)
}

func (b *archiveBackend) StatReference(ctx context.Context, ref consoleapi.ObjectRef) (consoleapi.ObjectInfo, error) {
	if b.stat != nil {
		return b.stat(ctx, ref)
	}
	return consoleapi.ObjectInfo{Size: 4, ETag: "fixed-etag", VersionID: "fixed-version"}, nil
}
func (b *archiveBackend) OpenReference(ctx context.Context, ref consoleapi.ObjectRef, etag string) (consoleapi.Object, error) {
	if b.read != nil {
		return b.read(ctx, ref, etag)
	}
	return consoleapi.Object{Body: io.NopCloser(strings.NewReader("data")), Size: 4}, nil
}
func (b *archiveBackend) ScanObjects(ctx context.Context, bucket, prefix, cursor string, limit int) (consoleapi.Page, error) {
	if b.scan != nil {
		return b.scan(ctx, bucket, prefix, cursor, limit)
	}
	return consoleapi.Page{}, nil
}

func archiveServer(t *testing.T, backend *archiveBackend, max int64) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Config{Backend: backend, Alias: "local", BaseURL: testOrigin, LoginCode: "test-login-code", ArchiveDir: dir, MaxArchiveSize: max})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}
func archiveRequest(s *Server, cookie *http.Cookie, csrf, method, target, body string) *httptest.ResponseRecorder {
	r := testRequest(method, target, body, cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func startArchive(t *testing.T, s *Server, cookie *http.Cookie, csrf, body string) archiveReply {
	t.Helper()
	w := archiveRequest(s, cookie, csrf, http.MethodPost, "/api/archives", body)
	if w.Code != 202 {
		t.Fatalf("start=%d %s", w.Code, w.Body.String())
	}
	var info archiveReply
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	return info
}
func awaitArchive(t *testing.T, s *Server, cookie *http.Cookie, id string) archiveReply {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		w := archiveRequest(s, cookie, "", http.MethodGet, "/api/archives/"+id, "")
		var info archiveReply
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &info) != nil {
			t.Fatalf("status=%d %s", w.Code, w.Body.String())
		}
		if info.Status != "planning" && info.Status != "running" {
			return info
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("archive did not finish")
	return archiveReply{}
}
func assertArchiveClean(t *testing.T, s *Server, dir string) {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || len(s.archiveSlots) != 0 {
		t.Fatalf("archive resources leaked: files=%d slots=%d", len(files), len(s.archiveSlots))
	}
}

func TestArchiveReadOnlyProducesPinnedSafeZipAndOneTimeDownload(t *testing.T) {
	backend := &archiveBackend{read: func(_ context.Context, ref consoleapi.ObjectRef, etag string) (consoleapi.Object, error) {
		if (ref.Key == "../../CON" && ref.VersionID != "fixed-version") || (ref.Key != "../../CON" && ref.VersionID != "") || etag != "fixed-etag" {
			t.Errorf("unfixed archive target %+v %q", ref, etag)
		}
		return consoleapi.Object{Body: io.NopCloser(strings.NewReader("data")), Size: 4}, nil
	}}
	s, dir := archiveServer(t, backend, 16)
	cookie, sess := signIn(t, s)
	info := startArchive(t, s, cookie, sess.CSRFToken, `{"refs":[{"bucket":"bucket","key":"../../CON","versionId":"fixed-version"},{"bucket":"bucket","key":"目录/a\\b?.txt"},{"bucket":"bucket","key":"../../CON","versionId":"fixed-version"}]}`)
	ready := awaitArchive(t, s, cookie, info.ID)
	if ready.Status != "ready" || ready.Count != 2 || ready.Size != 8 || ready.Transferred != 8 || len(ready.Entries) != 2 {
		t.Fatalf("unexpected ready %+v", ready)
	}
	if len(s.archiveSlots) != 1 {
		t.Fatal("ready file lost its disk reservation")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("expected one bounded temporary file")
	}
	stat, _ := files[0].Info()
	// Windows exposes writable files as 0666; Unix permission bits do not
	// describe its ACLs. Session isolation is checked on every platform below.
	if runtime.GOOS != "windows" && stat.Mode().Perm() != 0600 {
		t.Fatal("archive file is not private")
	}
	// A second session cannot inspect or consume another session's archive.
	other, _ := signIn(t, s)
	for _, suffix := range []string{"", "/download"} {
		if w := archiveRequest(s, other, "", http.MethodGet, "/api/archives/"+info.ID+suffix, ""); w.Code != 404 {
			t.Fatalf("foreign access=%d", w.Code)
		}
	}
	w := archiveRequest(s, cookie, "", http.MethodGet, "/api/archives/"+info.ID+"/download", "")
	if w.Code != 200 {
		t.Fatalf("download=%d %s", w.Code, w.Body.String())
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 3 {
		t.Fatalf("zip entries=%d", len(z.File))
	}
	for _, entry := range z.File {
		if path.Clean(entry.Name) != entry.Name || strings.HasPrefix(entry.Name, "/") || strings.Contains(entry.Name, "\\") || strings.Contains(entry.Name, "../") {
			t.Fatalf("unsafe ZIP path %q", entry.Name)
		}
		body, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name == "manifest.json" {
			var manifest struct {
				Version int
				Objects []archiveItem
			}
			if json.Unmarshal(data, &manifest) != nil || manifest.Version != 1 || len(manifest.Objects) != 2 || manifest.Objects[0].Key != "../../CON" || manifest.Objects[0].VersionID != "fixed-version" {
				t.Fatalf("manifest=%s", data)
			}
		} else if string(data) != "data" {
			t.Fatalf("wrong entry %q", data)
		}
	}
	finished := awaitArchive(t, s, cookie, info.ID)
	if finished.Status != "succeeded" {
		t.Fatalf("download outcome=%s", finished.Status)
	}
	if again := archiveRequest(s, cookie, "", http.MethodGet, "/api/archives/"+info.ID+"/download", ""); again.Code != 409 {
		t.Fatalf("archive was replayed: %d", again.Code)
	}
	assertArchiveClean(t, s, dir)
}

func TestArchiveFailuresNeverExposePartialDownload(t *testing.T) {
	for _, kind := range []string{"truncated", "quota", "unstable", "empty-prefix", "bad-prefix", "repeated-page"} {
		t.Run(kind, func(t *testing.T) {
			b := &archiveBackend{}
			max := int64(8)
			body := `{"refs":[{"bucket":"bucket","key":"file"}]}`
			switch kind {
			case "truncated":
				b.read = func(context.Context, consoleapi.ObjectRef, string) (consoleapi.Object, error) {
					return consoleapi.Object{Body: io.NopCloser(strings.NewReader("bad")), Size: 4}, nil
				}
			case "quota":
				max = 3
			case "unstable":
				b.stat = func(context.Context, consoleapi.ObjectRef) (consoleapi.ObjectInfo, error) {
					return consoleapi.ObjectInfo{Size: 4}, nil
				}
			case "empty-prefix", "bad-prefix", "repeated-page":
				body = `{"bucket":"bucket","prefix":"folder/"}`
				b.scan = func(context.Context, string, string, string, int) (consoleapi.Page, error) {
					if kind == "bad-prefix" {
						return consoleapi.Page{Entries: []consoleapi.Entry{{Key: "elsewhere"}}}, nil
					}
					if kind == "repeated-page" {
						return consoleapi.Page{NextCursor: "repeat"}, nil
					}
					return consoleapi.Page{}, nil
				}
			}
			s, dir := archiveServer(t, b, max)
			cookie, sess := signIn(t, s)
			info := startArchive(t, s, cookie, sess.CSRFToken, body)
			failed := awaitArchive(t, s, cookie, info.ID)
			if failed.Status != "failed" || failed.Error == nil {
				t.Fatalf("unexpected failure %+v", failed)
			}
			if w := archiveRequest(s, cookie, "", http.MethodGet, "/api/archives/"+info.ID+"/download", ""); w.Code != 409 {
				t.Fatal("partial archive downloadable")
			}
			assertArchiveClean(t, s, dir)
		})
	}
}

func TestArchiveMixedAuthorizationFailureRemovesPartialFilesAndReleasesSlot(t *testing.T) {
	for _, stage := range []string{"stat", "read"} {
		t.Run(stage, func(t *testing.T) {
			denied := &consoleapi.Error{Status: http.StatusForbidden, Code: "AccessDenied", Message: "Object access denied."}
			backend := &archiveBackend{}
			var dir string
			backend.stat = func(_ context.Context, ref consoleapi.ObjectRef) (consoleapi.ObjectInfo, error) {
				if ref.Key == "denied" && stage == "stat" {
					return consoleapi.ObjectInfo{}, denied
				}
				return consoleapi.ObjectInfo{Size: 4, ETag: "fixed-etag"}, nil
			}
			backend.read = func(_ context.Context, ref consoleapi.ObjectRef, _ string) (consoleapi.Object, error) {
				if ref.Key == "denied" {
					files, err := os.ReadDir(dir)
					if err != nil || len(files) != 1 {
						t.Errorf("expected a partial archive before read denial: files=%d error=%v", len(files), err)
					}
					return consoleapi.Object{}, denied
				}
				return consoleapi.Object{Body: io.NopCloser(strings.NewReader("data")), Size: 4}, nil
			}
			s, archiveDir := archiveServer(t, backend, 16)
			dir = archiveDir
			cookie, sess := signIn(t, s)
			info := startArchive(t, s, cookie, sess.CSRFToken, `{"refs":[{"bucket":"bucket","key":"allowed"},{"bucket":"bucket","key":"denied"}]}`)
			failed := awaitArchive(t, s, cookie, info.ID)
			if failed.Status != "failed" || failed.Error == nil || failed.Error.Code != "AccessDenied" {
				t.Fatalf("unexpected authorization failure %+v", failed)
			}
			if stage == "read" && failed.Transferred != 4 {
				t.Fatalf("read denial did not follow a copied object: %+v", failed)
			}
			if w := archiveRequest(s, cookie, "", http.MethodGet, "/api/archives/"+info.ID+"/download", ""); w.Code != http.StatusConflict {
				t.Fatalf("partial archive downloadable: %d", w.Code)
			}
			assertArchiveClean(t, s, dir)
			next := startArchive(t, s, cookie, sess.CSRFToken, `{"refs":[{"bucket":"bucket","key":"allowed"}]}`)
			if ready := awaitArchive(t, s, cookie, next.ID); ready.Status != "ready" {
				t.Fatalf("archive slot not reusable after denial: %+v", ready)
			}
			if w := archiveRequest(s, cookie, "", http.MethodGet, "/api/archives/"+next.ID+"/download", ""); w.Code != http.StatusOK {
				t.Fatalf("next archive download=%d", w.Code)
			}
			assertArchiveClean(t, s, dir)
		})
	}
}

type blockedArchiveBody struct {
	done chan struct{}
	once sync.Once
}

func (b *blockedArchiveBody) Read([]byte) (int, error) { <-b.done; return 0, io.ErrClosedPipe }
func (b *blockedArchiveBody) Close() error             { b.once.Do(func() { close(b.done) }); return nil }

func TestArchiveCancelAndLogoutCloseBlockedSources(t *testing.T) {
	for _, action := range []string{"cancel", "logout", "close"} {
		t.Run(action, func(t *testing.T) {
			opened := make(chan struct{})
			body := &blockedArchiveBody{done: make(chan struct{})}
			b := &archiveBackend{read: func(context.Context, consoleapi.ObjectRef, string) (consoleapi.Object, error) {
				close(opened)
				return consoleapi.Object{Body: body, Size: 4}, nil
			}}
			s, dir := archiveServer(t, b, 8)
			cookie, sess := signIn(t, s)
			info := startArchive(t, s, cookie, sess.CSRFToken, `{"refs":[{"bucket":"bucket","key":"file"}]}`)
			select {
			case <-opened:
			case <-time.After(time.Second):
				t.Fatal("source not opened")
			}
			busy := archiveRequest(s, cookie, sess.CSRFToken, http.MethodPost, "/api/archives", `{"refs":[{"bucket":"bucket","key":"next"}]}`)
			if busy.Code != 429 {
				t.Fatalf("archive slot was not bounded: %d", busy.Code)
			}
			switch action {
			case "cancel":
				if w := archiveRequest(s, cookie, sess.CSRFToken, http.MethodPost, "/api/archives/"+info.ID+"/cancel", ""); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			case "logout":
				if w := archiveRequest(s, cookie, sess.CSRFToken, http.MethodPost, "/api/logout", ""); w.Code != 204 {
					t.Fatal(w.Body.String())
				}
			case "close":
				_ = s.Close()
			}
			select {
			case <-body.done:
			case <-time.After(time.Second):
				t.Fatal("blocked source remained open")
			}
			s.workers.Wait()
			assertArchiveClean(t, s, dir)
		})
	}
}

func TestArchiveCreateRequiresSessionOriginAndCSRF(t *testing.T) {
	s, _ := archiveServer(t, &archiveBackend{}, 8)
	cookie, sess := signIn(t, s)
	for _, kind := range []string{"anonymous", "origin", "csrf"} {
		r := testRequest(http.MethodPost, "/api/archives", `{"refs":[{"bucket":"bucket","key":"file"}]}`, cookie)
		r.Header.Set("X-CSRF-Token", sess.CSRFToken)
		if kind == "anonymous" {
			r.Header.Del("Cookie")
		}
		if kind == "origin" {
			r.Header.Set("Origin", "http://evil.example")
		}
		if kind == "csrf" {
			r.Header.Del("X-CSRF-Token")
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("%s accepted %d", kind, w.Code)
		}
	}
	if len(s.archiveSlots) != 0 {
		t.Fatal("unauthorized request reserved archive resources")
	}
}

// A busy response can block on the browser after the task has been marked as
// downloading. Cancellation must still release its temporary file and slot.
type archiveBusyWriter struct {
	*httptest.ResponseRecorder
	written chan struct{}
	release chan struct{}
	fail    bool
}

func (w *archiveBusyWriter) Write(p []byte) (int, error) {
	close(w.written)
	if w.release != nil {
		<-w.release
	}
	if w.fail {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(p)
}

func TestArchiveBusyDownloadCancellationReleasesResources(t *testing.T) {
	s, dir := archiveServer(t, &archiveBackend{}, 8)
	cookie, sess := signIn(t, s)
	info := startArchive(t, s, cookie, sess.CSRFToken, `{"refs":[{"bucket":"bucket","key":"file"}]}`)
	if ready := awaitArchive(t, s, cookie, info.ID); ready.Status != "ready" {
		t.Fatalf("status=%s", ready.Status)
	}
	for i := 0; i < cap(s.downloads); i++ {
		s.downloads <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(s.downloads); i++ {
			<-s.downloads
		}
	}()
	writer := &archiveBusyWriter{ResponseRecorder: httptest.NewRecorder(), written: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.ServeHTTP(writer, testRequest(http.MethodGet, "/api/archives/"+info.ID+"/download", "", cookie))
	}()
	select {
	case <-writer.written:
	case <-time.After(time.Second):
		t.Fatal("busy download response did not start")
	}
	canceled := archiveRequest(s, cookie, sess.CSRFToken, http.MethodPost, "/api/archives/"+info.ID+"/cancel", "")
	if canceled.Code != 200 {
		t.Fatalf("cancel=%d %s", canceled.Code, canceled.Body.String())
	}
	close(writer.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("busy download request did not stop")
	}
	s.workers.Wait()
	assertArchiveClean(t, s, dir)
}

func TestArchiveBusyDownloadWriterFailureRestoresReady(t *testing.T) {
	s, dir := archiveServer(t, &archiveBackend{}, 8)
	cookie, sess := signIn(t, s)
	info := startArchive(t, s, cookie, sess.CSRFToken, `{"refs":[{"bucket":"bucket","key":"file"}]}`)
	if ready := awaitArchive(t, s, cookie, info.ID); ready.Status != "ready" {
		t.Fatalf("status=%s", ready.Status)
	}
	for i := 0; i < cap(s.downloads); i++ {
		s.downloads <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(s.downloads); i++ {
			<-s.downloads
		}
	}()
	writer := &archiveBusyWriter{ResponseRecorder: httptest.NewRecorder(), written: make(chan struct{}), fail: true}
	func() {
		defer func() {
			if recovered := recover(); recovered != http.ErrAbortHandler {
				t.Fatalf("abort=%v", recovered)
			}
		}()
		s.ServeHTTP(writer, testRequest(http.MethodGet, "/api/archives/"+info.ID+"/download", "", cookie))
	}()
	if status := awaitArchive(t, s, cookie, info.ID); status.Status != "ready" {
		t.Fatalf("failed busy response left archive in %s", status.Status)
	}
	if canceled := archiveRequest(s, cookie, sess.CSRFToken, http.MethodPost, "/api/archives/"+info.ID+"/cancel", ""); canceled.Code != 200 {
		t.Fatalf("cancel=%d", canceled.Code)
	}
	assertArchiveClean(t, s, dir)
}

type archiveFailedDownloadWriter struct {
	*httptest.ResponseRecorder
}

func (w *archiveFailedDownloadWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestArchiveFailedDownloadNeverReportsSuccess(t *testing.T) {
	s, dir := archiveServer(t, &archiveBackend{}, 8)
	cookie, sess := signIn(t, s)
	info := startArchive(t, s, cookie, sess.CSRFToken, `{"refs":[{"bucket":"bucket","key":"file"}]}`)
	if ready := awaitArchive(t, s, cookie, info.ID); ready.Status != "ready" {
		t.Fatalf("status=%s", ready.Status)
	}
	writer := &archiveFailedDownloadWriter{ResponseRecorder: httptest.NewRecorder()}
	s.ServeHTTP(writer, testRequest(http.MethodGet, "/api/archives/"+info.ID+"/download", "", cookie))
	failed := awaitArchive(t, s, cookie, info.ID)
	if failed.Status != "failed" || failed.Error == nil || failed.Error.Code != "archive_download_incomplete" {
		t.Fatalf("failed browser write outcome=%+v", failed)
	}
	assertArchiveClean(t, s, dir)
}

func TestArchiveListRestoresOwnedTasksAndSortsNewestStably(t *testing.T) {
	s, dir := archiveServer(t, &archiveBackend{}, 8)
	cookie, sess := signIn(t, s)
	first := startArchive(t, s, cookie, sess.CSRFToken, `{"refs":[{"bucket":"bucket","key":"first"}]}`)
	if ready := awaitArchive(t, s, cookie, first.ID); ready.Status != "ready" {
		t.Fatal(ready.Status)
	}
	if canceled := archiveRequest(s, cookie, sess.CSRFToken, http.MethodPost, "/api/archives/"+first.ID+"/cancel", ""); canceled.Code != 200 {
		t.Fatal(canceled.Body.String())
	}
	second := startArchive(t, s, cookie, sess.CSRFToken, `{"refs":[{"bucket":"bucket","key":"second"}]}`)
	if ready := awaitArchive(t, s, cookie, second.ID); ready.Status != "ready" {
		t.Fatal(ready.Status)
	}
	list := func(cookie *http.Cookie) []archiveReply {
		t.Helper()
		request := testRequest(http.MethodGet, "/api/archives", "", cookie)
		request.Header.Del("Origin")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, request)
		var result struct {
			Archives []archiveReply `json:"archives"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Archives == nil {
			t.Fatalf("list=%d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), dir) || strings.Contains(w.Body.String(), "secretKey") {
			t.Fatal("archive list disclosed local paths or credentials")
		}
		return result.Archives
	}
	owned := list(cookie)
	if len(owned) != 2 || owned[0].ID != second.ID || owned[0].Status != "ready" || owned[1].ID != first.ID {
		t.Fatalf("owned list=%+v", owned)
	}
	// The same browser cookie retrieves an earlier ready task; a new login's
	// cookie owns a different session and must see no prior archive metadata.
	other, _ := signIn(t, s)
	if foreign := list(other); len(foreign) != 0 {
		t.Fatalf("foreign archive metadata=%+v", foreign)
	}
	s.mu.Lock()
	s.archives[first.ID].info.Created = s.archives[second.ID].info.Created
	s.mu.Unlock()
	tied := list(cookie)
	if len(tied) != 2 || tied[0].ID < tied[1].ID {
		t.Fatalf("unstable same-time order=%+v", tied)
	}
	if anonymous := archiveRequest(s, nil, "", http.MethodGet, "/api/archives", ""); anonymous.Code != 401 {
		t.Fatalf("anonymous list=%d", anonymous.Code)
	}
	if query := archiveRequest(s, cookie, "", http.MethodGet, "/api/archives?user=other", ""); query.Code != 400 {
		t.Fatalf("query accepted=%d", query.Code)
	}
	if canceled := archiveRequest(s, cookie, sess.CSRFToken, http.MethodPost, "/api/archives/"+second.ID+"/cancel", ""); canceled.Code != 200 {
		t.Fatal(canceled.Body.String())
	}
	s.workers.Wait()
	assertArchiveClean(t, s, dir)
}

func TestArchiveListIsBoundedToSixteenOwnedTasks(t *testing.T) {
	s, dir := archiveServer(t, &archiveBackend{}, 8)
	cookie, sess := signIn(t, s)
	for i := 0; i < 16; i++ {
		info := startArchive(t, s, cookie, sess.CSRFToken, `{"refs":[{"bucket":"bucket","key":"file"}]}`)
		if canceled := archiveRequest(s, cookie, sess.CSRFToken, http.MethodPost, "/api/archives/"+info.ID+"/cancel", ""); canceled.Code != 200 {
			t.Fatalf("cancel=%d", canceled.Code)
		}
		s.workers.Wait()
	}
	if rejected := archiveRequest(s, cookie, sess.CSRFToken, http.MethodPost, "/api/archives", `{"refs":[{"bucket":"bucket","key":"next"}]}`); rejected.Code != 429 {
		t.Fatalf("17th task=%d", rejected.Code)
	}
	w := archiveRequest(s, cookie, "", http.MethodGet, "/api/archives", "")
	var result struct {
		Archives []archiveReply `json:"archives"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Archives) != 16 {
		t.Fatalf("bounded list=%d %s", w.Code, w.Body.String())
	}
	assertArchiveClean(t, s, dir)
}
