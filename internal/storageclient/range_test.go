// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soulteary/mc/internal/consoleapi"
)

func TestRangeChecksWireResponse(t *testing.T) {
	for _, tc := range []struct {
		name, contentRange, version, capability, etag string
		status                                        int
		ok                                            bool
	}{
		{"valid", "bytes 2-4/10", "null", "v1", "etag", 206, true}, {"ignored", "", "null", "v1", "etag", 200, false},
		{"wrong total", "bytes 2-4/11", "null", "v1", "etag", 206, false}, {"wrong range", "bytes 3-5/10", "null", "v1", "etag", 206, false},
		{"missing protection", "bytes 2-4/10", "null", "", "etag", 206, false}, {"wrong version", "bytes 2-4/10", "other", "v1", "etag", 206, false},
		{"changed", "bytes 2-4/10", "null", "v1", "other", 206, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Header.Get("Range") != "bytes=2-4" || r.Header.Get("If-Match") != "\"etag\"" || r.URL.Query().Get("versionId") != "null" {
					t.Error("lost pinned request")
				}
				w.Header().Set("Content-Range", tc.contentRange)
				w.Header().Set("X-Amz-Version-Id", tc.version)
				w.Header().Set("X-Otterio-Version-Authorization", tc.capability)
				w.Header().Set("ETag", "\""+tc.etag+"\"")
				w.Header().Set("Content-Length", "3")
				w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("234"))
			}))
			defer server.Close()
			c := testClient(t, Config{S3URL: server.URL})
			obj, err := c.OpenRange(context.Background(), consoleapi.ObjectRef{Bucket: "bucket", Key: "中文.txt", VersionID: "null"}, consoleapi.ObjectInfo{Size: 10, ETag: "etag"}, 2, 4)
			if (err == nil) != tc.ok {
				t.Fatalf("error=%v", err)
			}
			if obj.Body != nil {
				defer obj.Body.Close()
				data, _ := io.ReadAll(obj.Body)
				if string(data) != "234" || obj.Size != 3 {
					t.Fatal("wrong bytes")
				}
			}
		})
	}
}
