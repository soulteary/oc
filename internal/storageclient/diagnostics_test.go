// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticsWhitelist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/otterio/admin/v3/info" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("missing signed diagnostics request")
		}
		fmt.Fprint(w, `{"deploymentID":"secret-marker","usage":{"size":900},"servers":[{"state":"online","version":"release","uptime":12,"mem_stats":{"HeapAlloc":17},"endpoint":"secret-marker","drives":[{"path":"secret-marker"}],"env":{"secret":"secret-marker"}}]}`)
	}))
	defer server.Close()
	c := testClient(t, Config{S3URL: server.URL})
	result, err := c.ServerDiagnostics(context.Background())
	data, _ := json.Marshal(result)
	if err != nil || len(result.Nodes) != 1 || result.Nodes[0].HeapBytes != 17 || result.Nodes[0].Drives != 1 || strings.Contains(string(data), "secret-marker") || strings.Contains(string(data), "900") {
		t.Fatalf("unsafe or incorrect snapshot: %v", err)
	}
}

func TestDiagnosticsRejectsDeniedRedirectOversizeAndMalformed(t *testing.T) {
	for _, kind := range []string{"denied", "redirect", "oversize", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch kind {
				case "denied":
					w.WriteHeader(403)
					fmt.Fprint(w, "secret-marker")
				case "redirect":
					w.Header().Set("Location", "http://127.0.0.1:1/secret")
					w.WriteHeader(307)
				case "oversize":
					fmt.Fprint(w, strings.Repeat(" ", maxDiagnosticsReply+1))
				case "malformed":
					fmt.Fprint(w, `{"servers":[]} trailing`)
				}
			}))
			defer server.Close()
			c := testClient(t, Config{S3URL: server.URL})
			_, err := c.ServerDiagnostics(context.Background())
			if err == nil || calls != 1 || strings.Contains(err.Error(), "secret-marker") {
				t.Fatalf("unsafe error or retry: %v, calls=%d", err, calls)
			}
		})
	}
}

func TestDiagnosticsCancellationClosesUpstream(t *testing.T) {
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	c := testClient(t, Config{S3URL: server.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.ServerDiagnostics(ctx); err == nil {
		t.Fatal("canceled request succeeded")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("upstream not released")
	}
}

// This opt-in check is for isolated servers and temporary test credentials only.
func TestDiagnosticsRealServer(t *testing.T) {
	endpoint := os.Getenv("OC_DIAGNOSTICS_TEST_URL")
	if endpoint == "" {
		t.Skip("isolated real server not configured")
	}
	c, err := New(Config{S3URL: endpoint, AccessKey: os.Getenv("OC_DIAGNOSTICS_TEST_ACCESS"), SecretKey: os.Getenv("OC_DIAGNOSTICS_TEST_SECRET")})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	result, err := c.ServerDiagnostics(context.Background())
	if err != nil || len(result.Nodes) == 0 {
		t.Fatalf("real server snapshot failed: %v", err)
	}
	ctx := context.Background()
	if err := c.admin.AddUser(ctx, "diagnostic-viewer", "diagnostic-viewer-secret"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.admin.RemoveUser(ctx, "diagnostic-viewer"); err != nil {
			t.Error(err)
		}
	}()
	if err := c.admin.SetPolicy(ctx, "readwrite", "diagnostic-viewer", false); err != nil {
		t.Fatal(err)
	}
	denied, err := New(Config{S3URL: endpoint, AccessKey: "diagnostic-viewer", SecretKey: "diagnostic-viewer-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer denied.Close()
	_, err = denied.ServerDiagnostics(context.Background())
	assertAPIError(t, err, 403, "diagnostics_denied")
}
