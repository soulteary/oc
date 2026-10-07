//go:build !windows

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPipeS3SignalCancelsNetworkAndAborts(t *testing.T) {
	ready, released := make(chan struct{}), make(chan struct{})
	aborted := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Has("location"):
			fmt.Fprint(w, "<LocationConstraint>us-east-1</LocationConstraint>")
		case r.Method == http.MethodPost && r.URL.Query().Has("uploads"):
			fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>file</Key><UploadId>owned-upload</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut:
			io.Copy(io.Discard, r.Body)
			close(ready)
			<-r.Context().Done()
			close(released)
		case r.Method == http.MethodDelete:
			aborted <- r.URL.Query().Get("uploadId")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := newConfigV10()
	cfg.Aliases["store"] = aliasConfigV10{URL: server.URL, AccessKey: "test", SecretKey: "testtest", API: "S3v4", Path: "on"}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(root, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]string{"--config-dir", root, "--json", "pipe", "store/bucket/file"})
	executable, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestFSWatchCLIHelper$")
	command.Env = append(os.Environ(), "OC_TEST_WATCH_CLI_ARGS="+string(raw))
	command.Stdin = strings.NewReader("upload input")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("upload never reached network")
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err := command.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != globalTerminatExitStatus {
		t.Fatalf("expected 143, got %v", err)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("network request remains active")
	}
	select {
	case id := <-aborted:
		if id != "owned-upload" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("multipart session was not aborted")
	}
}
