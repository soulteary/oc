// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package clienttransport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type countedBody struct {
	io.ReadCloser
	closed bool
}

func (b *countedBody) Close() error { b.closed = true; return b.ReadCloser.Close() }

func TestNormalizeOnlySignedEmptyAdminBody(t *testing.T) {
	for _, test := range []struct {
		name, body, hash string
		length           int64
		wantNormalized   bool
	}{
		{"signed-empty", "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", 0, true},
		{"unsigned-empty", "", "UNSIGNED-PAYLOAD", 0, false},
		{"nonempty", "payload", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", 7, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &countedBody{ReadCloser: io.NopCloser(strings.NewReader(test.body))}
			req, err := http.NewRequest(http.MethodPut, "http://localhost/otterio/admin/v3/test", body)
			if err != nil {
				t.Fatal(err)
			}
			req.ContentLength = test.length
			req.TransferEncoding = []string{"chunked"}
			req.Header.Set("X-Amz-Content-Sha256", test.hash)
			normalized := NormalizeAdminRequest(req)
			if test.wantNormalized {
				if normalized == req || normalized.Body != http.NoBody || len(normalized.TransferEncoding) != 0 || !body.closed {
					t.Fatal("signed empty request was not normalized")
				}
				if req.Body != body || len(req.TransferEncoding) != 1 {
					t.Fatal("original request fields were modified")
				}
			} else if normalized != req || body.closed {
				t.Fatal("nonempty or differently signed request was modified")
			}
		})
	}
}

func TestAdminRedirectNeverReachesAnotherHost(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	for _, status := range []int{301, 302, 303, 307, 308} {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/credential-secret", status)
		}))
		transport := New(nil)
		client := &http.Client{Transport: Admin{Base: transport}}
		req, err := http.NewRequest(http.MethodPut, origin.URL, strings.NewReader("encrypted-body-secret"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "signed-credential-secret")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		transport.CloseIdleConnections()
		origin.Close()
		if err != nil || resp.StatusCode != http.StatusForbidden || resp.Header.Get("Location") != "" || !strings.Contains(string(body), "AdminRedirectDisabled") || strings.Contains(string(body), "credential-secret") {
			t.Fatalf("unexpected redirect normalization: status=%d", resp.StatusCode)
		}
	}
	if forwarded.Load() != 0 {
		t.Fatal("signed request reached redirected host")
	}
}

func TestRootEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{"http://localhost:9000", "https://example.com/", "https://[::1]:9001"} {
		if _, err := ValidateAdminEndpoint(endpoint); err != nil {
			t.Fatalf("valid endpoint rejected: %v", err)
		}
	}
	for _, endpoint := range []string{"http://user:secret@localhost", "http://localhost/admin", "http://localhost?secret=1", "http://localhost#", "http://localhost:", "http://localhost:0", "http://localhost:65536", "file:///tmp/data"} {
		if _, err := ValidateAdminEndpoint(endpoint); err == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
}
