// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"net/http/httptest"
	"testing"
)

func TestRegisteredRangeDownloadUsesAuthenticatedRoute(t *testing.T) {
	b := &rangeFake{size: 10}
	s, err := New(Config{Backend: b, Alias: "local", BaseURL: testOrigin, LoginCode: "test-login-code"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drainOwnedClients(t, s) })
	cookie, _ := signIn(t, s)
	for _, tc := range []struct {
		method, rangeHeader string
		cookie              bool
		status              int
		body                string
	}{
		{"GET", "bytes=2-4", false, 401, ""},
		{"GET", "bytes=2-4", true, 206, "234"},
		{"HEAD", "bytes=2-4", true, 200, ""},
		{"POST", "bytes=2-4", true, 405, ""},
	} {
		r := httptest.NewRequest(tc.method, testOrigin+"/api/download?bucket=bucket&key=key&versionId=null", nil)
		r.Header.Set("Range", tc.rangeHeader)
		if tc.cookie {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.method, w.Code, w.Body.String())
		}
		if tc.status == 206 && w.Body.String() != tc.body {
			t.Fatal("range route returned wrong bytes")
		}
		if tc.method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD returned a body")
		}
	}
}

func TestRegisteredDownloadIgnoresIfRangeWithoutRange(t *testing.T) {
	// A plain backend intentionally has no optional RangeBackend implementation.
	s, err := New(Config{Backend: &fakeBackend{}, Alias: "local", BaseURL: testOrigin, LoginCode: "test-login-code"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drainOwnedClients(t, s) })
	cookie, _ := signIn(t, s)
	r := httptest.NewRequest("GET", testOrigin+"/api/download?bucket=bucket&key=key", nil)
	r.AddCookie(cookie)
	r.Header.Set("If-Range", "\"unrelated-etag\"")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "object" {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestRegisteredRangeDownloadRetainsIfRange(t *testing.T) {
	s, err := New(Config{Backend: &rangeFake{size: 10}, Alias: "local", BaseURL: testOrigin, LoginCode: "test-login-code"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drainOwnedClients(t, s) })
	cookie, _ := signIn(t, s)
	for _, tc := range []struct {
		etag   string
		status int
		body   string
	}{
		{"\"etag\"", 206, "234"}, {"\"different\"", 200, "0123456789"},
	} {
		r := httptest.NewRequest("GET", testOrigin+"/api/download?bucket=bucket&key=key", nil)
		r.AddCookie(cookie)
		r.Header.Set("Range", "bytes=2-4")
		r.Header.Set("If-Range", tc.etag)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.status || w.Body.String() != tc.body {
			t.Fatalf("%s: status=%d body=%q", tc.etag, w.Code, w.Body.String())
		}
	}
}
