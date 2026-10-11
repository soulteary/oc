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

	"github.com/soulteary/otterio/pkg/madmin"
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

func TestDiagnosticsRejectsIncompleteSuccessfulReplies(t *testing.T) {
	cases := map[string]string{
		"null": "null", "empty": "{}", "servers-null": `{"servers":null}`, "servers-empty": `{"servers":[]}`, "null-node": `{"servers":[null]}`, "empty-node": `{"servers":[{}]}`,
		"missing-state":                `{"servers":[{"version":"release","mem_stats":{"HeapAlloc":0}}]}`,
		"null-state":                   `{"servers":[{"state":null,"version":"release","mem_stats":{"HeapAlloc":0}}]}`,
		"unknown-state":                `{"servers":[{"state":"secret-marker","version":"release","mem_stats":{"HeapAlloc":0}}]}`,
		"missing-version":              `{"servers":[{"state":"online","mem_stats":{"HeapAlloc":0}}]}`,
		"initializing-missing-version": `{"servers":[{"state":"initializing","mem_stats":{"HeapAlloc":0}}]}`,
		"empty-version":                `{"servers":[{"state":"online","version":"","mem_stats":{"HeapAlloc":0}}]}`,
		"null-version":                 `{"servers":[{"state":"offline","version":null,"mem_stats":{"HeapAlloc":0}}]}`,
		"missing-memory":               `{"servers":[{"state":"online","version":"release"}]}`,
		"null-memory":                  `{"servers":[{"state":"online","version":"release","mem_stats":null}]}`,
		"missing-heap":                 `{"servers":[{"state":"online","version":"release","mem_stats":{}}]}`,
		"null-heap":                    `{"servers":[{"state":"online","version":"release","mem_stats":{"HeapAlloc":null}}]}`,
		"negative-heap":                `{"servers":[{"state":"online","version":"release","mem_stats":{"HeapAlloc":-1}}]}`,
		"null-uptime":                  `{"servers":[{"state":"online","version":"release","uptime":null,"mem_stats":{"HeapAlloc":0}}]}`,
		"negative-uptime":              `{"servers":[{"state":"online","version":"release","uptime":-1,"mem_stats":{"HeapAlloc":0}}]}`,
		"null-drives":                  `{"servers":[{"state":"online","version":"release","mem_stats":{"HeapAlloc":0},"drives":null}]}`,
		"null-drive-entry":             `{"servers":[{"state":"offline","mem_stats":{"HeapAlloc":0},"drives":[null]}]}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, payload) }))
			defer server.Close()
			c := testClient(t, Config{S3URL: server.URL})
			snapshot, err := c.ServerDiagnostics(context.Background())
			assertAPIError(t, err, 502, "diagnostics_unavailable")
			if len(snapshot.Nodes) != 0 || strings.Contains(err.Error(), "secret-marker") {
				t.Fatal("incomplete snapshot or upstream details exposed")
			}
		})
	}
}

func TestDiagnosticsPreservesActualZeroAndOfflineProtocolShapes(t *testing.T) {
	for _, state := range []string{"online", "initializing", "offline"} {
		t.Run(state, func(t *testing.T) {
			node := madmin.ServerProperties{State: state}
			if state != "offline" {
				node.Version = "release"
			}
			payload, err := json.Marshal(madmin.InfoMessage{Servers: []madmin.ServerProperties{node}})
			if err != nil {
				t.Fatal(err)
			}
			// ServerProperties uses omitempty for uptime/drives, while zero MemStats is
			// serialized. Offline peers have neither version nor uptime information.
			if strings.Contains(string(payload), `"uptime"`) || strings.Contains(string(payload), `"drives"`) {
				t.Fatal("fixture did not exercise actual omitted zero fields")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(payload) }))
			defer server.Close()
			c := testClient(t, Config{S3URL: server.URL})
			result, err := c.ServerDiagnostics(context.Background())
			if err != nil || len(result.Nodes) != 1 {
				t.Fatalf("valid zero/offline shape rejected: %v", err)
			}
			actual := result.Nodes[0]
			if actual.State != state || actual.HeapBytes != 0 || actual.UptimeSeconds != 0 || actual.Drives != 0 || actual.Version != node.Version {
				t.Fatal("zero/offline fields fabricated or lost")
			}
		})
	}
}
