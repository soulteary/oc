// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/soulteary/mc/internal/consoleapi"
	minio "github.com/soulteary/otterio-sdk/v7"
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

// observedRangeTransport records closure of the actual SDK response stream,
// rather than relying only on the returned Object being empty on rejection.
type observedRangeTransport struct {
	base   http.RoundTripper
	closed *atomic.Bool
}

func (t observedRangeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err == nil && r.Method == http.MethodGet && !r.URL.Query().Has("location") {
		response.Body = &observedRangeBody{ReadCloser: response.Body, closed: t.closed}
	}
	return response, err
}

type observedRangeBody struct {
	io.ReadCloser
	closed *atomic.Bool
}

func (b *observedRangeBody) Close() error { b.closed.Store(true); return b.ReadCloser.Close() }

func TestRangePinsObservedCurrentVersion(t *testing.T) {
	for _, tc := range []struct {
		name, requested, headVersion, getVersion string
		ok                                       bool
	}{
		{"current unchanged", "", "v1", "v1", true},
		{"current same etag changed version", "", "v1", "v2", false},
		{"current version acknowledgement missing", "", "v1", "", false},
		{"unversioned unchanged", "", "", "", true},
		{"null normalized get", "", "", "null", true},
		{"null normalized head", "", "null", "", true},
		{"unversioned becomes versioned", "", "", "v2", false},
		{"explicit null no acknowledgement", "null", "", "", true},
		{"explicit null acknowledgement", "null", "null", "null", true},
		{"explicit historical unchanged", "v1", "v1", "v1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.URL.Query().Get("versionId") != tc.requested {
					t.Error("changed selected version")
				}
				w.Header().Set("ETag", "\"same-etag\"")
				w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
				w.Header().Set("X-Otterio-Version-Authorization", "v1")
				if r.Method == http.MethodHead {
					w.Header().Set("Content-Length", "10")
					w.Header().Set("X-Amz-Version-Id", tc.headVersion)
					return
				}
				if r.Header.Get("Range") != "bytes=2-4" || r.Header.Get("If-Match") != "\"same-etag\"" {
					t.Error("lost byte or ETag pin")
				}
				w.Header().Set("Content-Length", "3")
				w.Header().Set("Content-Range", "bytes 2-4/10")
				w.Header().Set("X-Amz-Version-Id", tc.getVersion)
				w.WriteHeader(206)
				_, _ = w.Write([]byte("234"))
			}))
			defer server.Close()
			c := testClient(t, Config{S3URL: server.URL})
			closed := &atomic.Bool{}
			opts := c.s3Options
			opts.Transport = observedRangeTransport{base: opts.Transport, closed: closed}
			var err error
			c.s3, err = minio.New(c.s3Endpoint, &opts)
			if err != nil {
				t.Fatal(err)
			}
			ref := consoleapi.ObjectRef{Bucket: "bucket", Key: "key", VersionID: tc.requested}
			metadata, err := c.StatReference(context.Background(), ref)
			if err != nil {
				t.Fatal(err)
			}
			object, err := c.OpenRange(context.Background(), ref, metadata, 2, 4)
			if tc.ok {
				if err != nil {
					t.Fatal(err)
				}
				data, readErr := io.ReadAll(object.Body)
				_ = object.Body.Close()
				if readErr != nil || string(data) != "234" {
					t.Fatalf("data=%q error=%v", data, readErr)
				}
			} else {
				var apiErr *consoleapi.Error
				if !errors.As(err, &apiErr) || apiErr.Code != "object_changed" || object.Body != nil {
					t.Fatalf("object=%+v error=%v", object, err)
				}
			}
			if !closed.Load() {
				t.Fatal("upstream body was not closed")
			}
		})
	}
}
