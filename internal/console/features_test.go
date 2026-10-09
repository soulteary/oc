// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type fakeFeatureBackend struct {
	*fakeMutationBackend
	creates, removes, versionReads, shares atomic.Int32
	create                                 func(context.Context, string) error
	remove                                 func(context.Context, string) error
	versions                               func(context.Context, string, string, string, int) (consoleapi.VersionPage, error)
	share                                  func(context.Context, consoleapi.ShareRequest) (consoleapi.Share, error)
}

func (b *fakeFeatureBackend) CreateBucket(ctx context.Context, bucket string) error {
	b.creates.Add(1)
	if b.create != nil {
		return b.create(ctx, bucket)
	}
	return nil
}
func (b *fakeFeatureBackend) DeleteBucket(ctx context.Context, bucket string) error {
	b.removes.Add(1)
	if b.remove != nil {
		return b.remove(ctx, bucket)
	}
	return nil
}
func (b *fakeFeatureBackend) ListVersions(ctx context.Context, bucket, key, cursor string, limit int) (consoleapi.VersionPage, error) {
	b.versionReads.Add(1)
	if b.versions != nil {
		return b.versions(ctx, bucket, key, cursor, limit)
	}
	return consoleapi.VersionPage{}, nil
}
func (b *fakeFeatureBackend) Presign(ctx context.Context, args consoleapi.ShareRequest) (consoleapi.Share, error) {
	b.shares.Add(1)
	if b.share != nil {
		return b.share(ctx, args)
	}
	return consoleapi.Share{URL: "https://s3.example.com/signed", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func featureServer(t *testing.T, writes, sharing bool) (*Server, *fakeFeatureBackend, *http.Cookie, sessionReply) {
	t.Helper()
	b := &fakeFeatureBackend{fakeMutationBackend: &fakeMutationBackend{fakeBackend: &fakeBackend{}}}
	s, err := New(Config{Backend: b, Alias: "local", BaseURL: testOrigin, LoginCode: "test-login-code", AllowWrites: writes, AllowSharing: sharing})
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
	cookie, reply := signIn(t, s)
	return s, b, cookie, reply
}

func TestFeatureBucketsRequireWriteGateExactConfirmationAndValidNames(t *testing.T) {
	s, b, cookie, reply := featureServer(t, false, false)
	valid := `{"bucket":"exact-bucket"}`
	if w := jobRequest(s, http.MethodPost, "/api/buckets/create", valid, cookie, reply.CSRFToken); w.Code != 403 {
		t.Fatal("read-only mode created a bucket")
	}
	s.writer = b
	for _, body := range []string{`{"bucket":"ab"}`, `{"bucket":"127.0.0.1"}`, `{"bucket":"bad..name"}`, `{"bucket":"-bad-name"}`, `{"bucket":"good-bucket","extra":true}`, `{"bucket":"good-bucket"} {}`, `{"bucket":"good-bucket","confirmBucket":"other"}`} {
		if w := jobRequest(s, http.MethodPost, "/api/buckets/create", body, cookie, reply.CSRFToken); w.Code != 400 {
			t.Fatalf("accepted create body %s: %d", body, w.Code)
		}
	}
	if w := jobRequest(s, http.MethodPost, "/api/buckets/create", valid, cookie, reply.CSRFToken); w.Code != 200 {
		t.Fatalf("create failed: %s", w.Body.String())
	}
	for _, body := range []string{`{"bucket":"exact-bucket"}`, `{"bucket":"exact-bucket","confirmBucket":"other"}`} {
		if w := jobRequest(s, http.MethodPost, "/api/buckets/delete", body, cookie, reply.CSRFToken); w.Code != 400 {
			t.Fatal("delete accepted absent or incorrect confirmation")
		}
	}
	b.remove = func(context.Context, string) error {
		return &consoleapi.Error{Status: 409, Code: "bucket_not_empty", Message: "The bucket is not empty."}
	}
	w := jobRequest(s, http.MethodPost, "/api/buckets/delete", `{"bucket":"exact-bucket","confirmBucket":"exact-bucket"}`, cookie, reply.CSRFToken)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "bucket_not_empty") || b.creates.Load() != 1 || b.removes.Load() != 1 {
		t.Fatal("invalid bucket request reached backend or conflict was lost")
	}
}

func TestFeatureMutationsRequireSessionOriginCSRFAndBoundedBody(t *testing.T) {
	s, b, cookie, reply := featureServer(t, true, true)
	for _, route := range []string{"/api/buckets/create", "/api/buckets/delete", "/api/shares"} {
		for _, omitted := range []string{"cookie", "origin", "csrf"} {
			r := testRequest(http.MethodPost, route, `{}`, cookie)
			r.Header.Set("X-CSRF-Token", reply.CSRFToken)
			switch omitted {
			case "cookie":
				r.Header.Del("Cookie")
			case "origin":
				r.Header.Del("Origin")
			case "csrf":
				r.Header.Del("X-CSRF-Token")
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != 401 && w.Code != 403 {
				t.Fatalf("%s accepted missing %s: %d", route, omitted, w.Code)
			}
		}
		w := jobRequest(s, http.MethodPost, route, `{"padding":"`+strings.Repeat("x", 10000)+`"}`, cookie, reply.CSRFToken)
		if w.Code != 400 && w.Code != 413 {
			t.Fatalf("%s accepted excessive body: %d", route, w.Code)
		}
	}
	if b.creates.Load()+b.removes.Load()+b.shares.Load() != 0 {
		t.Fatal("rejected mutation reached backend")
	}
}

func TestFeatureVersionsAreReadOnlyExactAndRejectUnknownOrDuplicateQueries(t *testing.T) {
	s, b, cookie, _ := featureServer(t, false, false)
	b.versions = func(_ context.Context, bucket, key, cursor string, limit int) (consoleapi.VersionPage, error) {
		if bucket != "bucket" || key != "key+literal" || cursor != "opaque" || limit != 2 {
			t.Fatal("version query changed")
		}
		return consoleapi.VersionPage{}, nil
	}
	w := jobRequest(s, http.MethodGet, "/api/versions?bucket=bucket&key=key%2Bliteral&cursor=opaque&limit=2", "", cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"entries":[]`) {
		t.Fatalf("read-only versions failed: %s", w.Body.String())
	}
	for _, q := range []string{"bucket=bucket", "bucket=bucket&key=", "bucket=bucket&key=key&key=other", "bucket=bucket&key=key&alias=other", "bucket=bucket&key=key&limit=1001", "bucket=bucket&key=key&cursor=one&cursor=two"} {
		w = jobRequest(s, http.MethodGet, "/api/versions?"+q, "", cookie, "")
		if w.Code != 400 {
			t.Fatalf("accepted query %s: %d", q, w.Code)
		}
	}
	if b.versionReads.Load() != 1 {
		t.Fatal("rejected query reached backend")
	}
}

func TestFeatureShareGateIndependentOfWritesAndPreservesVersion(t *testing.T) {
	s, b, cookie, reply := featureServer(t, false, false)
	body := `{"bucket":"bucket","key":"key","versionId":"null"}`
	if w := jobRequest(s, http.MethodPost, "/api/shares", body, cookie, reply.CSRFToken); w.Code != 403 {
		t.Fatal("default sharing gate missing")
	}
	s.allowSharing = true
	b.share = func(_ context.Context, args consoleapi.ShareRequest) (consoleapi.Share, error) {
		if args.Bucket != "bucket" || args.Key != "key" || args.VersionID != "null" || args.ExpiresSeconds != 3600 {
			t.Fatal("share reference/default expiry changed")
		}
		return consoleapi.Share{URL: "https://s3.example.com/signed?versionId=null", ExpiresAt: time.Unix(123, 0).UTC()}, nil
	}
	w := jobRequest(s, http.MethodPost, "/api/shares", body, cookie, reply.CSRFToken)
	var share consoleapi.Share
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &share) != nil || share.URL == "" || share.ExpiresAt.Unix() != 123 {
		t.Fatalf("read-only signed share failed: %s", w.Body.String())
	}
	for _, body := range []string{`{"bucket":"bucket","key":"key","expiresSeconds":604801}`, `{"bucket":"bucket","key":"key","expiresSeconds":-1}`, `{"bucket":"bucket","key":"key","versionId":"a\r\nb"}`, `{"bucket":"bucket","key":"key","downloadName":"../key"}`, `{"bucket":"bucket","key":"key","method":"PUT"}`} {
		if w := jobRequest(s, http.MethodPost, "/api/shares", body, cookie, reply.CSRFToken); w.Code != 400 {
			t.Fatalf("accepted share body %s: %d", body, w.Code)
		}
	}
	if b.shares.Load() != 1 {
		t.Fatal("rejected share reached backend")
	}
}

func TestFeatureLogoutCancelsActiveVersionPage(t *testing.T) {
	s, b, cookie, reply := featureServer(t, false, false)
	started := make(chan struct{})
	b.versions = func(ctx context.Context, _, _, _ string, _ int) (consoleapi.VersionPage, error) {
		close(started)
		<-ctx.Done()
		return consoleapi.VersionPage{}, ctx.Err()
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- jobRequest(s, http.MethodGet, "/api/versions?bucket=bucket&key=key", "", cookie, "") }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("version read did not start")
	}
	if w := jobRequest(s, http.MethodPost, "/api/logout", "", cookie, reply.CSRFToken); w.Code != 204 {
		t.Fatal("logout failed")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session logout did not cancel versions")
	}
}
