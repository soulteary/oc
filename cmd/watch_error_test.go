package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/cli"
	"github.com/soulteary/mc/pkg/probe"
)

func TestWatchStreamTermination(t *testing.T) {
	for _, kind := range []string{"closed", "deadline", "canceled", "closed-canceled", "closed-deadline"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if strings.Contains(kind, "deadline") {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			}
			defer cancel()
			wo := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error)}
			if strings.Contains(kind, "closed") {
				close(wo.EventInfoChan)
				close(wo.ErrorChan)
			}
			if strings.Contains(kind, "canceled") {
				cancel()
			}
			err := watchNotifications(ctx, wo)
			if strings.Contains(kind, "canceled") {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			code, ok := err.(cli.ExitCoder)
			if !ok || code.ExitCode() != 1 {
				t.Fatalf("expected failure, got %v", err)
			}
		})
	}
	code, category := classifyClientError(fmt.Errorf("wrapped: %w", errWatchStreamClosed))
	if code != "WatchStreamClosed" || category != "watch" {
		t.Fatalf("%s %s", code, category)
	}
}

func TestWrappedAPINotImplementedClassification(t *testing.T) {
	for _, err := range []error{
		APINotImplemented{API: "Watch", APIType: "S3"},
		&APINotImplemented{API: "Watch", APIType: "S3"},
	} {
		code, category := classifyClientError(fmt.Errorf("wrapped: %w", err))
		if code != "NotImplemented" || category != "unsupported" {
			t.Fatalf("%T: code=%q category=%q", err, code, category)
		}
	}
}

func TestWatchErrorCLIHelper(t *testing.T) {
	if os.Getenv("OC_TEST_WATCH_ERROR_HELPER") != "1" {
		return
	}
	args := []string{"oc", "--config-dir", os.Getenv("OC_TEST_WATCH_ERROR_CONFIG")}
	if os.Getenv("OC_TEST_WATCH_ERROR_TEXT") != "1" {
		args = append(args, "--json")
	}
	Main(append(args, "watch", "store/bucket"))
	os.Exit(0)
}

func TestWatchErrorsCLI(t *testing.T) {
	for _, tc := range []struct {
		code, category string
		status         int
	}{
		{"NotImplemented", "unsupported", http.StatusNotImplemented},
		{"AccessDenied", "permission", http.StatusForbidden},
		{"InvalidAccessKeyId", "authentication", http.StatusForbidden},
		{"NoSuchBucket", "not_found", http.StatusNotFound},
	} {
		for _, text := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/text=%t", tc.code, text), func(t *testing.T) {
				testWatchErrorCLI(t, tc.code, tc.category, tc.status, text)
			})
		}
	}
}

func testWatchErrorCLI(t *testing.T, code, category string, status int, text bool) {
	t.Helper()
	testWatchHandlerCLI(t, code, category, text, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Has("location") {
			fmt.Fprint(w, "<LocationConstraint>us-east-1</LocationConstraint>")
			return
		}
		w.WriteHeader(status)
		fmt.Fprintf(w, "<Error><Code>%s</Code><Message>watch unavailable</Message></Error>", code)
	}))
}

func TestWatchBrokenStreamCLI(t *testing.T) {
	for _, kind := range []string{"malformed", "truncated", "reconnect-error"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			code, category := "", "other"
			if kind == "reconnect-error" {
				code, category = "NotImplemented", "unsupported"
			}
			testWatchHandlerCLI(t, code, category, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("location") {
					fmt.Fprint(w, "<LocationConstraint>us-east-1</LocationConstraint>")
					return
				}
				switch kind {
				case "malformed":
					fmt.Fprintln(w, "invalid-json")
				case "truncated":
					w.Header().Set("Content-Length", "1000")
					fmt.Fprint(w, "{\"Records\":[]}\n")
				case "reconnect-error":
					if requests.Add(1) == 1 {
						w.WriteHeader(http.StatusOK)
						return
					}
					w.WriteHeader(http.StatusNotImplemented)
					fmt.Fprint(w, "<Error><Code>NotImplemented</Code><Message>watch unavailable</Message></Error>")
				}
			}))
		})
	}
}

func testWatchHandlerCLI(t *testing.T, code, category string, text bool, handler http.Handler) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := newWatchErrorCommand(t, ctx, server.URL, text)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("expected exit 1, got %v: %s %s", err, output, stderr.String())
	}
	if text {
		if !strings.Contains(string(output)+stderr.String(), "Unable to watch for events.") {
			t.Fatalf("missing error message: %s %s", output, stderr.String())
		}
		return
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected JSON stderr: %s", stderr.String())
	}
	var report struct {
		Status string `json:"status"`
		Error  struct {
			Code     string `json:"code"`
			Category string `json:"category"`
		} `json:"error"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("invalid or duplicate JSON output: %v: %s", err, output)
	}
	if report.Status != "error" || report.Error.Code != code || report.Error.Category != category {
		t.Fatalf("unexpected report: %s", output)
	}
}

func newWatchErrorCommand(t *testing.T, ctx context.Context, endpoint string, text bool) *exec.Cmd {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(configV10{Version: "10", Aliases: map[string]aliasConfigV10{
		"store": {URL: endpoint, AccessKey: "test", SecretKey: "test-secret", API: "S3v4", Path: "on"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, executable, "-test.run=^TestWatchErrorCLIHelper$")
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "OC_HOST_store", "MC_HOST_store", "OC_HOSTS_store", "MC_HOSTS_store", "OC_TEST_WATCH_ERROR_HELPER", "OC_TEST_WATCH_ERROR_CONFIG", "OC_TEST_WATCH_ERROR_TEXT":
			continue
		}
		command.Env = append(command.Env, entry)
	}
	command.Env = append(command.Env, "OC_TEST_WATCH_ERROR_HELPER=1", "OC_TEST_WATCH_ERROR_CONFIG="+dir)
	if text {
		command.Env = append(command.Env, "OC_TEST_WATCH_ERROR_TEXT=1")
	}
	return command
}
