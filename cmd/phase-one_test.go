package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/cli"
	"github.com/soulteary/otterio/pkg/madmin"
)

func TestDisabledSelfUpdate(t *testing.T) {
	ctx := cli.NewContext(cli.NewApp(), flag.NewFlagSet("update", flag.ContinueOnError), nil)
	err := mainUpdate(ctx)
	code, ok := err.(cli.ExitCoder)
	if !ok || code.ExitCode() != 1 || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected explicit disabled error, got %v", err)
	}
}

func TestHealthUploadFlagsRejected(t *testing.T) {
	for _, args := range [][]string{nil, {"--license=test"}, {"--license="}, {"--dev"}, {"--dev=false"}} {
		fs := flag.NewFlagSet("health", flag.ContinueOnError)
		fs.String("license", "", "")
		fs.Bool("dev", false, "")
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		ctx := cli.NewContext(cli.NewApp(), fs, nil)
		err := rejectHealthUpload(ctx)
		if (err != nil) != (len(args) > 0) {
			t.Fatalf("args=%v error=%v", args, err)
		}
	}
}

func TestTraceRedactionDoesNotMutateRequest(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://user:password@example.com/object?X-Amz-Signature=signature-secret&X-Amz-Credential=credential-secret&token=token-secret&prefix=kept", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=mixed_key, Signature=auth-secret")
	req.Header.Set("X-Amz-Security-Token", "session-secret")
	req.Header.Set("Cookie", "cookie-secret")
	req.Header.Set("X-Amz-Server-Side-Encryption-Customer-Key", "key-secret")
	originalURL := req.URL.String()
	originalAuth := req.Header.Get("Authorization")
	copied := redactTraceRequest(req)
	serialized := copied.URL.String() + copied.Header.Get("Authorization") + copied.Header.Get("X-Amz-Security-Token") + copied.Header.Get("Cookie") + copied.Header.Get("X-Amz-Server-Side-Encryption-Customer-Key")
	for _, secret := range []string{"password", "signature-secret", "credential-secret", "token-secret", "mixed_key", "auth-secret", "session-secret", "cookie-secret", "key-secret"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("trace leaked %q", secret)
		}
	}
	if copied.URL.Query().Get("prefix") != "kept" {
		t.Fatal("non-sensitive query removed")
	}
	if req.URL.String() != originalURL || req.Header.Get("Authorization") != originalAuth {
		t.Fatal("original request changed")
	}
}

func TestTraceResponseRedaction(t *testing.T) {
	resp := &http.Response{Header: http.Header{"Set-Cookie": {"response-secret"}, "Location": {"https://example.com/?X-Amz-Signature=redirect-secret"}}}
	copied := redactTraceResponse(resp)
	if copied.Header.Get("Set-Cookie") != traceRedacted || strings.Contains(copied.Header.Get("Location"), "redirect-secret") {
		t.Fatal("response secrets leaked")
	}
	if resp.Header.Get("Set-Cookie") != "response-secret" {
		t.Fatal("original response changed")
	}
}

func TestAliasOutputRedaction(t *testing.T) {
	msg := aliasMessage{op: "list", Alias: "test", URL: "https://user:password@example.com", AccessKey: "access-secret", SecretKey: "secret-secret"}
	for _, output := range []string{msg.String(), msg.JSON()} {
		for _, secret := range []string{"password", "access-secret", "secret-secret"} {
			if strings.Contains(output, secret) {
				t.Fatalf("alias output leaked %q", secret)
			}
		}
	}
	if msg.SecretKey != "secret-secret" {
		t.Fatal("stored credentials changed")
	}
}

// Verify the pinned SDK's actual request prefix and DTO decoding against the
// OtterIO wire contract; no production server or credentials are used.
func TestPinnedOtterioAdminContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/otterio/admin/v3/info" {
			t.Errorf("unexpected admin request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("missing v4 authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"mode": "server", "servers": []map[string]interface{}{{"endpoint": "localhost:9000", "state": "ok", "version": "2026-10-04T21-53-41Z"}}})
	}))
	defer server.Close()
	client, err := madmin.New(strings.TrimPrefix(server.URL, "http://"), "test-access", "test-secret", false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.ServerInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode != "server" || len(info.Servers) != 1 || info.Servers[0].Version != "2026-10-04T21-53-41Z" {
		t.Fatalf("unexpected DTO: %+v", info)
	}
}
