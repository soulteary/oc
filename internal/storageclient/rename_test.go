// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRenameRequiresSeparatePerRequestCapabilities(t *testing.T) {
	for _, test := range []struct {
		name, api string
		copy, del []string
		location  bool
	}{
		{name: "create-only-is-insufficient"},
		{name: "copy-without-delete", copy: []string{"v1"}},
		{name: "delete-without-copy", del: []string{"v1"}},
		{name: "unknown-copy", copy: []string{"v2"}, del: []string{"v1"}},
		{name: "unknown-delete", copy: []string{"v1"}, del: []string{"v2"}},
		{name: "duplicate-copy", copy: []string{"v1", "v1"}, del: []string{"v1"}},
		{name: "duplicate-delete", copy: []string{"v1"}, del: []string{"v1", "v1"}},
		{name: "location-cannot-grant-capability", location: true},
		{name: "signature-v2", api: "s3v2", copy: []string{"v1"}, del: []string{"v1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.location && r.URL.Query().Has("location") {
					w.Header().Set(conditionalCopyHeader, "v1")
					w.Header().Set(conditionalDeleteHeader, "v1")
				}
				if locationResponse(w, r) {
					return
				}
				if r.Method != http.MethodHead {
					writes.Add(1)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("X-Otterio-Conditional-Writes", "v1")
				w.Header()[http.CanonicalHeaderKey(conditionalCopyHeader)] = test.copy
				w.Header()[http.CanonicalHeaderKey(conditionalDeleteHeader)] = test.del
				if r.URL.Path == "/bucket/new" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("ETag", `"old-etag"`)
				w.Header().Set("Content-Length", "3")
				w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL, API: test.api})
			assertAPIError(t, client.RenameObject(context.Background(), "bucket", "old", "new", "old-etag"), http.StatusNotImplemented, "rename_unsupported")
			if writes.Load() != 0 {
				t.Fatal("unsupported rename dispatched a mutation")
			}
		})
	}
}

func TestRenameUsesConditionalDeleteAndPreservesPartialOutcomes(t *testing.T) {
	for _, test := range []struct {
		name                 string
		copyStatus           int
		copyMissingETag      bool
		copyInvalidETag      string
		copyLostResponse     bool
		deleteStatus         int
		deleteLostResponse   bool
		sourceChanges        bool
		wantStatus           int
		wantCode             string
		wantCopies, wantDels int32
	}{
		{name: "success", deleteStatus: 204, wantCopies: 1, wantDels: 1},
		{name: "destination-created-during-copy", copyStatus: 412, wantStatus: 409, wantCode: "rename_conflict"},
		{name: "copy-denied", copyStatus: 403, wantStatus: 403, wantCode: "AccessDenied"},
		{name: "copy-missing-acknowledgement", copyMissingETag: true, wantStatus: 502, wantCode: "outcome_unknown", wantCopies: 1},
		{name: "copy-whitespace-validator", copyInvalidETag: "invalid tag", wantStatus: 502, wantCode: "outcome_unknown", wantCopies: 1},
		{name: "copy-wildcard-validator", copyInvalidETag: "*", wantStatus: 502, wantCode: "outcome_unknown", wantCopies: 1},
		{name: "copy-weak-validator", copyInvalidETag: `W/"weak"`, wantStatus: 502, wantCode: "outcome_unknown", wantCopies: 1},
		{name: "copy-lost-response", copyLostResponse: true, wantStatus: 502, wantCode: "outcome_unknown", wantCopies: 1},
		{name: "source-changes-before-delete", sourceChanges: true, wantStatus: 409, wantCode: "rename_partial", wantCopies: 1, wantDels: 1},
		{name: "source-disappears-before-delete", deleteStatus: 404, wantStatus: 409, wantCode: "rename_partial", wantCopies: 1, wantDels: 1},
		{name: "delete-denied", deleteStatus: 403, wantStatus: 502, wantCode: "rename_partial", wantCopies: 1, wantDels: 1},
		{name: "delete-redirect", deleteStatus: 307, wantStatus: 502, wantCode: "rename_partial", wantCopies: 1, wantDels: 1},
		{name: "delete-invalid-success", deleteStatus: 200, wantStatus: 502, wantCode: "rename_partial", wantCopies: 1, wantDels: 1},
		{name: "delete-lost-response", deleteLostResponse: true, wantStatus: 502, wantCode: "rename_partial", wantCopies: 1, wantDels: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			const sourceKey = "folder//../中文 空格+?#%2F"
			const targetKey = "other//../新 名+?#%2F"
			var heads, puts, copies, deletes, redirects atomic.Int32
			var changed, removed atomic.Bool
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				redirects.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer redirect.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.URL.Query().Has("versionId") || r.Header.Get("X-Amz-Bypass-Governance-Retention") != "" || r.Header.Get("X-Minio-Force-Delete") != "" {
					t.Error("rename requested a version purge or retention bypass")
				}
				switch r.Method {
				case http.MethodHead:
					heads.Add(1)
					if r.URL.Path == "/bucket/"+targetKey {
						w.Header().Set(conditionalCopyHeader, "v1")
						w.WriteHeader(http.StatusNotFound)
						return
					}
					if r.URL.Path != "/bucket/"+sourceKey {
						t.Error("HEAD changed the source key")
					}
					w.Header().Set(conditionalDeleteHeader, "v1")
					w.Header().Set("ETag", `"old-etag"`)
					w.Header().Set("Content-Length", "3")
					w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
				case http.MethodPut:
					puts.Add(1)
					copySource, err := url.PathUnescape(r.Header.Get("X-Amz-Copy-Source"))
					if err != nil || strings.TrimPrefix(copySource, "/") != "bucket/"+sourceKey || r.URL.Path != "/bucket/"+targetKey {
						t.Error("copy changed an opaque source or destination key")
					}
					if r.Header.Get("If-None-Match") != "*" || r.Header.Get("X-Amz-Copy-Source-If-Match") != `"old-etag"` || !strings.Contains(r.Header.Get("Authorization"), "if-none-match") || !strings.Contains(r.Header.Get("Authorization"), "x-amz-copy-source-if-match") {
						t.Error("copy lost a signed source or destination condition")
					}
					if test.copyStatus != 0 {
						code := "PreconditionFailed"
						if test.copyStatus == 403 {
							code = "AccessDenied"
						}
						w.WriteHeader(test.copyStatus)
						fmt.Fprintf(w, `<Error><Code>%s</Code></Error>`, code)
						return
					}
					copies.Add(1)
					changed.Store(test.sourceChanges)
					if test.copyLostResponse {
						loseRenameResponse(t, w)
						return
					}
					etag := `"copied-etag"`
					if test.copyMissingETag {
						etag = ""
					}
					if test.copyInvalidETag != "" {
						etag = test.copyInvalidETag
					}
					fmt.Fprintf(w, `<CopyObjectResult><ETag>%s</ETag><LastModified>2026-10-08T00:00:00Z</LastModified></CopyObjectResult>`, etag)
				case http.MethodDelete:
					deletes.Add(1)
					if r.URL.Path != "/bucket/"+sourceKey || r.Header.Get("If-Match") != `"old-etag"` || r.URL.Query().Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" || !strings.Contains(r.URL.Query().Get("X-Amz-SignedHeaders"), "if-match") || r.Header.Get("If-None-Match") != "" {
						t.Error("source delete lost its signed condition or exact target")
					}
					if changed.Load() {
						w.WriteHeader(http.StatusPreconditionFailed)
						fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code></Error>`)
						return
					}
					if test.deleteLostResponse {
						loseRenameResponse(t, w)
						return
					}
					if test.deleteStatus == 307 {
						http.Redirect(w, r, redirect.URL, http.StatusTemporaryRedirect)
						return
					}
					removed.Store(test.deleteStatus == 204)
					w.WriteHeader(test.deleteStatus)
				default:
					t.Errorf("unexpected rename request: %s", r.Method)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			err := client.RenameObject(context.Background(), "bucket", sourceKey, targetKey, `"old-etag"`)
			if test.wantStatus != 0 {
				assertAPIError(t, err, test.wantStatus, test.wantCode)
			} else if err != nil || !removed.Load() {
				t.Fatalf("rename failed: %v", err)
			}
			if heads.Load() != 2 || puts.Load() != 1 || copies.Load() != test.wantCopies || deletes.Load() != test.wantDels || redirects.Load() != 0 {
				t.Fatalf("rename requests: heads=%d puts=%d copies=%d deletes=%d redirects=%d", heads.Load(), puts.Load(), copies.Load(), deletes.Load(), redirects.Load())
			}
			if test.sourceChanges && removed.Load() {
				t.Fatal("a concurrently updated source was deleted")
			}
		})
	}
}

func loseRenameResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	connection, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	_ = connection.Close()
}

func TestRenameRejectsWildcardBeforeDispatch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	assertAPIError(t, client.RenameObject(context.Background(), "bucket", "source", "target", "*"), http.StatusBadRequest, "InvalidRequest")
	if requests.Load() != 0 {
		t.Fatal("a wildcard rename reached storage")
	}
}
