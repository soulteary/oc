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
