package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdminPolicyUpdateRejectsInvalidInputBeforeReplacingBindings(t *testing.T) {
	for _, entity := range []string{"user", "group"} {
		for _, policy := range []string{"readonly", "", "  ", "readwrite,readonly", "readwrite"} {
			t.Run(entity+"/"+policy, func(t *testing.T) {
				var writes atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/otterio/admin/v3/user-info":
						_, _ = w.Write([]byte(`{"policyName":"readonly","status":"enabled"}`))
					case r.Method == http.MethodGet && r.URL.Path == "/otterio/admin/v3/group":
						_, _ = w.Write([]byte(`{"name":"alice","policy":"readonly","status":"enabled"}`))
					case r.Method == http.MethodPut && r.URL.Path == "/otterio/admin/v3/set-user-or-group-policy":
						writes.Add(1)
						if r.URL.Query().Get("policyName") != "readonly,readwrite" || r.URL.Query().Get("isGroup") != map[string]string{"user": "false", "group": "true"}[entity] {
							t.Errorf("unsafe binding replacement: %s", r.URL.RawQuery)
						}
					default:
						t.Errorf("unexpected policy request: %s %s", r.Method, r.URL.Path)
						w.WriteHeader(400)
					}
				}))
				defer server.Close()
				root := t.TempDir()
				cfg := newConfigV10()
				cfg.Aliases["store"] = aliasConfigV10{URL: server.URL, AccessKey: "policy-test", SecretKey: "policy-test-secret", API: "S3v4", Path: "on"}
				data, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "config.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal([]string{"--config-dir", root, "--json", "admin", "policy", "update", "store", policy, entity + "=alice"})
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, executable, "-test.run=^TestFSWatchCLIHelper$")
				command.Env = append(os.Environ(), "OC_TEST_WATCH_CLI_ARGS="+string(raw))
				output, err := command.CombinedOutput()
				if policy == "readwrite" {
					if err != nil || writes.Load() != 1 {
						t.Fatalf("valid policy append failed: %v writes=%d %s", err, writes.Load(), output)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 1 || writes.Load() != 0 || !strings.Contains(string(output), "Unable to update the policy") {
						t.Fatalf("invalid policy reached replacement: %v writes=%d %s", err, writes.Load(), output)
					}
				}
			})
		}
	}
}
