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
		{"Bytes=0-0", 10, 0, 0, true}, {"BYTES=4-", 10, 4, 9, true}, {"bytes=0-0", 10, 0, 0, true}, {"bytes=4-", 10, 4, 9, true}, {"bytes=-4", 10, 6, 9, true}, {"bytes=-99", 10, 0, 9, true}, {"bytes=3-99", 10, 3, 9, true},
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
	etag  string
}

func (f *rangeFake) StatReference(context.Context, consoleapi.ObjectRef) (consoleapi.ObjectInfo, error) {
	etag := f.etag
	if etag == "" {
		etag = "etag"
	}
	return consoleapi.ObjectInfo{Size: f.size, ETag: etag, Modified: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}, nil
}
func (f *rangeFake) OpenReference(_ context.Context, ref consoleapi.ObjectRef, etag string) (consoleapi.Object, error) {
	f.opens++
	f.ref = ref
	return consoleapi.Object{Body: io.NopCloser(strings.NewReader("0123456789")), Size: f.size}, nil
}
func (f *rangeFake) OpenRangeFull(ctx context.Context, ref consoleapi.ObjectRef, info consoleapi.ObjectInfo) (consoleapi.Object, error) {
	return f.OpenReference(ctx, ref, info.ETag)
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

func TestEntityTagGrammar(t *testing.T) {
	for _, tc := range []struct {
		list, tag            string
		weak, matched, valid bool
	}{
		{`"a,b"`, `"a,b"`, false, true, true},
		{`"other", W/"a,b"`, `"a,b"`, true, true, true},
		{`W/"a,b"`, `"a,b"`, false, false, true},
		{`"a\b"`, `"a\b"`, false, true, true},
		{`*`, `"tag"`, false, true, true},
		{`"tag", invalid`, `"tag"`, false, false, false},
		{`"tag",`, `"tag"`, false, false, false},
		{`*, "tag"`, `"tag"`, true, false, false},
		{`"a b"`, `"tag"`, true, false, false},
		{"\"a\x7fb\"", `"tag"`, true, false, false},
		{`"tag" junk`, `"tag"`, true, false, false},
		{`"unclosed`, `"tag"`, false, false, false},
	} {
		matched, valid := matchEntityTags(tc.list, tc.tag, tc.weak)
		if matched != tc.matched || valid != tc.valid {
			t.Fatalf("%q matched=%v valid=%v", tc.list, matched, valid)
		}
	}
}

func TestHTTPConditionsPreserveOpaqueETags(t *testing.T) {
	for _, opaque := range []string{"a,b", `a\b`} {
		for _, tc := range []struct {
			header, value string
			status        int
		}{
			{"If-None-Match", `W/"` + opaque + `"`, 304},
			{"If-Match", `"other", "` + opaque + `"`, 200},
			{"If-Range", `"` + opaque + `"`, 206},
			{"If-None-Match", `"` + opaque + `", malformed`, 400},
		} {
			f := &rangeFake{size: 10, etag: opaque}
			s := &Server{streamIdle: time.Second}
			sess := &session{runtime: sessionRuntime{backend: f}}
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set(tc.header, tc.value)
			if tc.header == "If-Range" {
				r.Header.Set("Range", "bytes=2-4")
			}
			w := httptest.NewRecorder()
			s.downloadRange(w, r, r.Context(), sess, consoleapi.ObjectRef{Bucket: "bucket", Key: "key"})
			if w.Code != tc.status || w.Header().Get("ETag") != `"`+opaque+`"` {
				t.Fatalf("%q %s: status=%d etag=%q", opaque, tc.header, w.Code, w.Header().Get("ETag"))
			}
		}
	}
}

type changedFullRangeFake struct {
	rangeFake
	fullCalled bool
}

func (f *changedFullRangeFake) OpenRangeFull(context.Context, consoleapi.ObjectRef, consoleapi.ObjectInfo) (consoleapi.Object, error) {
	f.fullCalled = true
	return consoleapi.Object{}, &consoleapi.Error{Status: 409, Code: "object_changed", Message: "Object changed."}
}
func TestConditionalFullDownloadUsesObservedMetadata(t *testing.T) {
	for _, header := range []string{"If-Match", "If-Range"} {
		f := &changedFullRangeFake{rangeFake: rangeFake{size: 10}}
		s := &Server{streamIdle: time.Second}
		sess := &session{runtime: sessionRuntime{backend: f}}
		r := httptest.NewRequest("GET", "/", nil)
		if header == "If-Match" {
			r.Header.Set(header, `"etag"`)
		} else {
			r.Header.Set("Range", "bytes=2-4")
			r.Header.Set(header, `"other"`)
		}
		w := httptest.NewRecorder()
		s.downloadRange(w, r, r.Context(), sess, consoleapi.ObjectRef{Bucket: "bucket", Key: "key"})
		if !f.fullCalled || w.Code != 409 || f.opens != 0 {
			t.Fatalf("%s: pin=%v status=%d oldopens=%d", header, f.fullCalled, w.Code, f.opens)
		}
	}
}

func TestRepeatedEntityTagFields(t *testing.T) {
	for _, tc := range []struct {
		header string
		values []string
		status int
	}{
		{"If-Match", []string{`"other"`, `"a,b"`}, 200},
		{"If-None-Match", []string{`"other"`, `W/"a,b"`}, 304},
		{"If-Match", []string{`"a,b"`, "malformed"}, 400},
	} {
		f := &rangeFake{size: 10, etag: "a,b"}
		s := &Server{streamIdle: time.Second}
		sess := &session{runtime: sessionRuntime{backend: f}}
		r := httptest.NewRequest("GET", "/", nil)
		for _, value := range tc.values {
			r.Header.Add(tc.header, value)
		}
		w := httptest.NewRecorder()
		s.downloadRange(w, r, r.Context(), sess, consoleapi.ObjectRef{Bucket: "bucket", Key: "key"})
		if w.Code != tc.status {
			t.Fatalf("%s %v: got %d want %d", tc.header, tc.values, w.Code, tc.status)
		}
	}
}

func TestRepeatedRangeFieldsRejected(t *testing.T) {
	f := &rangeFake{size: 10, etag: "etag"}
	s := &Server{streamIdle: time.Second}
	sess := &session{runtime: sessionRuntime{backend: f}}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Add("Range", "bytes=0-0")
	r.Header.Add("Range", "5-5")
	w := httptest.NewRecorder()
	s.downloadRange(w, r, r.Context(), sess, consoleapi.ObjectRef{Bucket: "bucket", Key: "key"})
	if w.Code != 416 {
		t.Fatalf("got %d want 416", w.Code)
	}
}
