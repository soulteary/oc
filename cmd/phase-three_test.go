package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/soulteary/otterio/pkg/madmin"
)

func TestClientEnvironmentPrecedence(t *testing.T) {
	t.Setenv("MC_REGION", "legacy")
	t.Setenv("OC_REGION", "preferred")
	if clientEnv("MC_REGION") != "preferred" {
		t.Fatal("OC value must win")
	}
	t.Setenv("OC_REGION", "")
	if clientEnv("MC_REGION") != "" {
		t.Fatal("explicit empty OC value must win")
	}
	if err := os.Unsetenv("OC_REGION"); err != nil {
		t.Fatal(err)
	}
	if clientEnv("MC_REGION") != "legacy" {
		t.Fatal("MC fallback missing")
	}
	t.Setenv("MC_HOST_store", "http://old:secret@localhost:9000")
	t.Setenv("OC_HOST_store", "http://new:secret@localhost:9001")
	value, ok := lookupClientEnv("MC_HOST_store")
	if !ok || value != "http://new:secret@localhost:9001" {
		t.Fatal("alias precedence broken")
	}
}

func TestOCConfigDirectoryIndependentOfExecutable(t *testing.T) {
	want := ".oc/"
	if runtime.GOOS == "windows" {
		want = "oc\\"
	}
	if got := defaultMCConfigDir(); got != want {
		t.Fatalf("directory %q, want %q", got, want)
	}
}

func TestClientConfigImportBackupAndValidation(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "mc", "config.json")
	dest := filepath.Join(root, "oc", "config.json")
	for _, path := range []string{source, dest} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	original := []byte(`{"version":"10","aliases":{"store":{"url":"http://localhost:9000","accessKey":"key","secretKey":"secret","api":"S3v4","path":"on","adminURL":"https://localhost:9001","adminCAFile":"ca.pem"}}}`)
	previous := []byte(`{"version":"10","aliases":{}}`)
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, previous, 0600); err != nil {
		t.Fatal(err)
	}
	count, err := importClientConfig(source, dest)
	if err != nil || count != 1 {
		t.Fatalf("import: %d %v", count, err)
	}
	unchanged, _ := os.ReadFile(source)
	if string(unchanged) != string(original) {
		t.Fatal("source modified")
	}
	backups, _ := filepath.Glob(dest + ".backup-*")
	if len(backups) != 1 {
		t.Fatal("missing backup")
	}
	backup, _ := os.ReadFile(backups[0])
	if string(backup) != string(previous) {
		t.Fatal("backup mismatch")
	}
	imported, _ := os.ReadFile(dest)
	var cfg configV10
	if err = json.Unmarshal(imported, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Aliases["store"].AdminCAFile != filepath.Join(filepath.Dir(source), "ca.pem") {
		t.Fatal("relative CA not preserved")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(dest)
		if info.Mode().Perm() != 0600 {
			t.Fatal("credentials file is not private")
		}
	}
	for _, bad := range []string{`{"version":"10","aliases":{"store":{"url":"http://localhost:badport","api":"S3v4"}}}`, `{"version":"10","aliases":{"store":{"url":"http://bad host","api":"S3v4"}}}`, `{"version":"10","aliases":{"store":{"url":"http://localhost?x=1","api":"S3v4"}}}`, `{"version":"10","aliases":{"store":{"url":"http://localhost#fragment","api":"S3v4"}}}`, `{"version":"9","aliases":{}}`, `{"version":"10"}`, `{"version":"10","aliases":{"bad/name":{"url":"http://localhost","api":"S3v4"}}}`, `{"version":"10","aliases":{"store":{"url":"http://localhost","api":"S3v4","adminURL":"http://user:secret@localhost"}}}`} {
		if err = os.WriteFile(source, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = importClientConfig(source, dest); err == nil {
			t.Fatal("invalid source accepted")
		}
		current, _ := os.ReadFile(dest)
		if string(current) != string(imported) {
			t.Fatal("invalid import modified destination")
		}
	}
	if _, err = importClientConfig(dest, dest); err == nil {
		t.Fatal("self-import accepted")
	}
}

func TestClientErrorClassification(t *testing.T) {
	for _, tc := range []struct{ code, category string }{
		{"AccessDenied", "permission"}, {"SignatureDoesNotMatch", "authentication"},
		{"NotImplemented", "unsupported"}, {"NoSuchKey", "not_found"}, {"AdminRedirectDisabled", "endpoint"},
	} {
		code, category := classifyClientError(fmt.Errorf("wrapped: %w", madmin.ErrorResponse{Code: tc.code, Message: "failure"}))
		if code != tc.code || category != tc.category {
			t.Fatalf("%s: %s %s", tc.code, code, category)
		}
	}
	_, category := classifyClientError(context.Canceled)
	if category != "canceled" {
		t.Fatal(category)
	}
}

func TestServiceCredentialValidation(t *testing.T) {
	for _, pair := range [][2]string{{"", ""}, {"service", "secret-key"}} {
		if err := validateServiceCredentials(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range [][2]string{{"", "secret-key"}, {"service", ""}, {"ab", "secret-key"}, {"service", "short"}, {"service", "12345678901234567890123456789012345678901"}} {
		if err := validateServiceCredentials(pair[0], pair[1]); err == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
}

func TestConsoleLogTypeMigration(t *testing.T) {
	for input, want := range map[string]string{"minio": "otterio", "OtterIO": "otterio", "application": "application", "all": "all"} {
		got, err := normalizeConsoleLogType(input)
		if err != nil || got != want {
			t.Fatalf("%s: %s %v", input, got, err)
		}
	}
	if _, err := normalizeConsoleLogType("unknown"); err == nil {
		t.Fatal("invalid log type accepted")
	}
}

func TestImportEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{"http://localhost:9000", "https://[::1]:9001/"} {
		if err := validateImportEndpoint(endpoint); err != nil {
			t.Fatalf("valid endpoint rejected: %v", err)
		}
	}
	for _, endpoint := range []string{"http://::1", "http://[invalid]:9000", "http://localhost:badport", "http://bad host", "http://localhost?x=1", "http://localhost#fragment", "http://localhost#", "http://localhost:", "http://localhost:0", "http://localhost:65536", "http://user:secret@localhost", "http://localhost/bucket"} {
		if err := validateImportEndpoint(endpoint); err == nil {
			t.Fatalf("invalid endpoint accepted: %q", endpoint)
		}
	}
}

func TestRestartRequiresNewInstances(t *testing.T) {
	before := madmin.InfoMessage{Mode: "online", Servers: []madmin.ServerProperties{{Endpoint: "one", Uptime: 20, State: "online"}, {Endpoint: "two", Uptime: 20, State: "online"}}}
	after := before
	after.Servers = append([]madmin.ServerProperties(nil), before.Servers...)
	if restartedServers(before, after, time.Second) {
		t.Fatal("old ready instances accepted")
	}
	after.Servers[0].Uptime = 0
	if restartedServers(before, after, time.Second) {
		t.Fatal("partial restart accepted")
	}
	after.Servers[1].Uptime = 0
	if !restartedServers(before, after, time.Second) {
		t.Fatal("new instances rejected")
	}
	after.Servers[1].State = "offline"
	if restartedServers(before, after, time.Second) {
		t.Fatal("offline instance accepted")
	}
	after.Servers[1].State = "online"
	after.Servers[1].Endpoint = "unknown"
	if restartedServers(before, after, time.Second) {
		t.Fatal("changed membership accepted")
	}
}
