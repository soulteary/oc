// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

func TestByteRanges(t *testing.T) {
	for _, tc := range []struct {
		value            string
		size, start, end int64
		ok               bool
	}{
		{"bytes=0-0", 10, 0, 0, true}, {"bytes=4-", 10, 4, 9, true}, {"bytes=-4", 10, 6, 9, true}, {"bytes=-99", 10, 0, 9, true}, {"bytes=3-99", 10, 3, 9, true},
		{"bytes=10-", 10, 0, 0, false}, {"bytes=0-0", 0, 0, 0, false}, {"bytes=-0", 10, 0, 0, false}, {"bytes=3-2", 10, 0, 0, false}, {"bytes=0-1,3-4", 10, 0, 0, false}, {"bytes=+1-2", 10, 0, 0, false}, {"bytes=9223372036854775808-", 10, 0, 0, false},
	} {
		a, b, ok := byteRange(tc.value, tc.size)
		if a != tc.start || b != tc.end || ok != tc.ok {
			t.Errorf("%q: %d %d %v", tc.value, a, b, ok)
		}
	}
}

type rangeFake struct {
	fakeBackend
	opens int
	ref   consoleapi.ObjectRef
	size  int64
}

func (f *rangeFake) StatReference(context.Context, consoleapi.ObjectRef) (consoleapi.ObjectInfo, error) {
	return consoleapi.ObjectInfo{Size: f.size, ETag: "etag", Modified: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}, nil
}
func (f *rangeFake) OpenReference(_ context.Context, ref consoleapi.ObjectRef, etag string) (consoleapi.Object, error) {
	f.opens++
	f.ref = ref
	return consoleapi.Object{Body: io.NopCloser(strings.NewReader("0123456789")), Size: f.size}, nil
}
func (f *rangeFake) OpenRange(_ context.Context, ref consoleapi.ObjectRef, _ consoleapi.ObjectInfo, a, b int64) (consoleapi.Object, error) {
	f.opens++
	f.ref = ref
	return consoleapi.Object{Body: io.NopCloser(strings.NewReader("0123456789"[a : b+1])), Size: b - a + 1}, nil
}
func TestRangeHTTPConditions(t *testing.T) {
	for _, tc := range []struct {
		method, header, value string
		status                int
		body                  string
		opens                 int
	}{
		{"GET", "Range", "bytes=2-4", 206, "234", 1}, {"GET", "Range", "bytes=-2", 206, "89", 1}, {"GET", "Range", "bytes=10-", 416, "", 0},
		{"HEAD", "Range", "bytes=2-4", 200, "", 0}, {"GET", "If-None-Match", "W/\"etag\"", 304, "", 0}, {"GET", "If-Match", "W/\"etag\"", 412, "", 0},
		{"GET", "If-Unmodified-Since", "Wed, 01 Jan 2025 00:00:00 GMT", 412, "", 0}, {"GET", "If-Modified-Since", "Thu, 01 Oct 2026 00:00:00 GMT", 304, "", 0},
		{"GET", "If-Range", "\"different\"", 200, "0123456789", 1}, {"GET", "If-Range", "\"etag\"", 206, "234", 1},
	} {
		t.Run(tc.header+tc.value, func(t *testing.T) {
			f := &rangeFake{size: 10}
			s := &Server{streamIdle: time.Second}
			sess := &session{runtime: sessionRuntime{backend: f}}
			r := httptest.NewRequest(tc.method, "/", nil)
			r.Header.Set(tc.header, tc.value)
			if tc.header == "If-Range" {
				r.Header.Set("Range", "bytes=2-4")
			}
			w := httptest.NewRecorder()
			s.downloadRange(w, r, r.Context(), sess, consoleapi.ObjectRef{Bucket: "bucket", Key: "key", VersionID: "null"})
			if w.Code != tc.status || w.Body.String() != tc.body || f.opens != tc.opens {
				t.Fatalf("status=%d body=%q opens=%d", w.Code, w.Body.String(), f.opens)
			}
			if f.opens > 0 && f.ref.VersionID != "null" {
				t.Fatal("lost null version")
			}
			if tc.status == http.StatusPartialContent && w.Header().Get("Content-Range") == "" {
				t.Fatal("missing range")
			}
		})
	}
}

func TestRangeResponsePreservesControllerAndLength(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapped := rangeResponse{recorder}
	if wrapped.Unwrap() != recorder {
		t.Fatal("response controller cannot reach underlying writer")
	}
	wrapped.WriteHeader(http.StatusBadGateway)
	if recorder.Code != http.StatusBadGateway {
		t.Fatal("converted upstream error to partial success")
	}
	recorder = httptest.NewRecorder()
	s := &Server{streamIdle: time.Second}
	complete := s.downloadUsing(rangeResponse{recorder}, context.Background(), "key", func(context.Context) (consoleapi.Object, error) {
		return consoleapi.Object{Body: io.NopCloser(strings.NewReader("x")), Size: 3}, nil
	})
	if complete || recorder.Code != 206 || recorder.Header().Get("Content-Length") != "3" || recorder.Body.String() != "x" {
		t.Fatal("truncated range reported complete")
	}
}
