// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soulteary/mc/internal/consoleapi"
)

// Exercise the actual SDK requests against a server that rejects destination
// conditions on non-committing multipart operations, as OtterIO does. In
// particular, this must pass with the released SDK pinned in go.mod.
func TestUploadConditionOnlyOnCommit(t *testing.T) {
	for _, test := range []struct {
		name          string
		size          int64
		overwrite     bool
		conflict      bool
		embeddedError bool
	}{
		{name: "single-create", size: 7},
		{name: "single-boundary-create", size: uploadPartSize},
		{name: "multipart-create", size: uploadPartSize + 1},
		{name: "single-overwrite", size: 7, overwrite: true},
		{name: "single-boundary-overwrite", size: uploadPartSize, overwrite: true},
		{name: "multipart-overwrite", size: uploadPartSize + 1, overwrite: true},
		{name: "single-boundary-conflict", size: uploadPartSize, conflict: true},
		{name: "multipart-conflict", size: uploadPartSize + 1, conflict: true},
		{name: "multipart-embedded-conflict", size: uploadPartSize + 1, conflict: true, embeddedError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var heads, initiates, parts, commits, aborts atomic.Int32
			var replaced atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.URL.Path != "/bucket/object" {
					t.Errorf("unexpected object path: %s", r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				query := r.URL.Query()
				isSingle := r.Method == http.MethodPut && !query.Has("uploadId")
				isComplete := r.Method == http.MethodPost && query.Get("uploadId") == "owned"
				condition := r.Header.Get("If-None-Match")
				wantCondition := ""
				if (isSingle || isComplete) && !test.overwrite {
					wantCondition = "*"
				}
				if condition != wantCondition || r.Header.Get("If-Match") != "" {
					t.Errorf("%s %s condition = %q, want %q", r.Method, r.URL.RequestURI(), condition, wantCondition)
					w.WriteHeader(http.StatusNotImplemented)
					fmt.Fprint(w, `<Error><Code>NotImplemented</Code></Error>`)
					return
				}
				if strings.Contains(r.Header.Get("Authorization"), "if-none-match") != (wantCondition != "") {
					t.Error("destination condition and signed headers disagree")
				}
				if isSingle || (r.Method == http.MethodPost && query.Has("uploads")) {
					if r.Header.Get("Content-Type") != "application/octet-stream" {
						t.Error("object metadata was lost")
					}
				}
				switch {
				case r.Method == http.MethodHead:
					heads.Add(1)
					w.Header().Set("X-Otterio-Conditional-Writes", "v1")
					w.WriteHeader(http.StatusNotFound)
				case r.Method == http.MethodPost && query.Has("uploads"):
					initiates.Add(1)
					fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>owned</UploadId></InitiateMultipartUploadResult>`)
				case r.Method == http.MethodPut && query.Get("uploadId") == "owned":
					number := parts.Add(1)
					if query.Get("partNumber") != strconv.Itoa(int(number)) {
						t.Error("part was retried or reordered")
					}
					wantSize := int64(uploadPartSize)
					if number == 2 {
						wantSize = 1
					}
					if size, err := io.Copy(io.Discard, r.Body); err != nil || size != wantSize {
						t.Errorf("part size = %d, want %d; error: %v", size, wantSize, err)
					}
					w.Header().Set("ETag", fmt.Sprintf(`"part-%d"`, number))
				case isSingle || isComplete:
					commits.Add(1)
					size, err := io.Copy(io.Discard, r.Body)
					if err != nil || (isSingle && size != test.size) {
						t.Errorf("invalid committing request body: size %d, error %v", size, err)
					}
					if test.conflict {
						// A competing writer creates the destination after our HEAD.
						// Completion must report the conflict without a second write.
						if !test.embeddedError {
							w.WriteHeader(http.StatusPreconditionFailed)
						}
						fmt.Fprint(w, " \n<Error><Code>PreconditionFailed</Code><Message>Destination exists</Message></Error>")
						return
					}
					replaced.Store(true)
					if isSingle {
						w.Header().Set("ETag", `"uploaded"`)
					} else {
						fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>bucket</Bucket><Key>object</Key><ETag>uploaded</ETag></CompleteMultipartUploadResult>`)
					}
				case r.Method == http.MethodDelete && query.Get("uploadId") == "owned":
					aborts.Add(1)
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			result, err := client.Upload(context.Background(), "bucket", "object", &generatedUploadReader{remaining: test.size}, test.size, consoleapi.UploadOptions{Overwrite: test.overwrite}, nil)
			if test.conflict {
				assertAPIError(t, err, http.StatusConflict, "object_exists")
				if replaced.Load() || result.ETag != "" {
					t.Fatal("conflict overwrote or acknowledged the competing object")
				}
			} else if err != nil || result.Size != test.size || result.ETag != "uploaded" || !replaced.Load() {
				t.Fatalf("upload result = %#v, error = %v", result, err)
			}
			var wantHeads, wantInitiates, wantParts, wantAborts int32
			if !test.overwrite {
				wantHeads = 1
			}
			if test.size > uploadPartSize {
				wantInitiates, wantParts = 1, 2
				if test.conflict {
					wantAborts = 1
				}
			}
			if heads.Load() != wantHeads || initiates.Load() != wantInitiates || parts.Load() != wantParts || commits.Load() != 1 || aborts.Load() != wantAborts {
				t.Fatalf("requests: heads=%d initiates=%d parts=%d commits=%d aborts=%d; want %d/%d/%d/1/%d",
					heads.Load(), initiates.Load(), parts.Load(), commits.Load(), aborts.Load(), wantHeads, wantInitiates, wantParts, wantAborts)
			}
		})
	}
}
