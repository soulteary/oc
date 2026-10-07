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
	"testing"
	"time"

	"github.com/soulteary/mc/pkg/probe"
)

type endedWatchClient struct {
	Client
	watch *WatchObject
}

func (c endedWatchClient) Watch(context.Context, WatchOptions) (*WatchObject, *probe.Error) {
	return c.watch, nil
}

func TestWatcherReportsUnexpectedEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error)}
	watcher := NewWatcher(time.Now())
	if err := watcher.Join(ctx, endedWatchClient{watch: source}, true); err != nil {
		t.Fatal(err)
	}
	close(source.EventInfoChan)
	close(source.ErrorChan)
	select {
	case err := <-watcher.Errors():
		if !errors.Is(err.ToGoError(), errWatchStreamClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription end was not propagated")
	}
	watcher.Wait()
}

func TestMirrorPeriodicCheckSkipsIdenticalBytes(t *testing.T) {
	previous := loadMcConfig
	loadMcConfig = func() (*configV10, *probe.Error) { return newConfigV10(), nil }
	defer func() { loadMcConfig = previous }()
	source, target := t.TempDir(), t.TempDir()
	for _, path := range []string{filepath.Join(source, "file"), filepath.Join(target, "file")} {
		if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		n := 0
		for result := range prepareMirrorURLs(context.Background(), source, target, mirrorOptions{isOverwrite: true, verifyContents: true}) {
			if result.Error != nil {
				t.Fatal(result.Error)
			}
			if result.SourceContent != nil {
				n++
			}
		}
		return n
	}
	if n := count(); n != 0 {
		t.Fatalf("unchanged files queued: %d", n)
	}
	if err := os.WriteFile(filepath.Join(target, "file"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("same-size corruption not repaired: %d", n)
	}
}

func TestMirrorRestartsEndedSubscription(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := NewWatcher(time.Now())
	job := &mirrorJob{watcher: watcher, statusCh: make(chan URLs, 1), stopCh: make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); job.watchMirror(ctx, func() {}) }()
	watcher.ErrorChan <- probe.NewError(errWatchStreamClosed)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ended subscription did not trigger restart")
	}
	if !job.rescanRequired {
		t.Fatal("restart does not reconcile event gap")
	}
}

func TestConsoleConnectionFailureCLI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()
	root := t.TempDir()
	cfg := newConfigV10()
	cfg.Aliases["store"] = aliasConfigV10{URL: endpoint, AccessKey: "test", SecretKey: "testtest", API: "S3v4", Path: "on"}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(root, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]string{"--config-dir", root, "--json", "admin", "console", "store"})
	executable, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestFSWatchCLIHelper$")
	command.Env = append(os.Environ(), "OC_TEST_WATCH_CLI_ARGS="+string(raw))
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "Unable to listen to console logs") {
		t.Fatalf("connection failure not reported: %v %s", err, output)
	}
}

func TestPipeNormalInputCLI(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "out")
	raw, _ := json.Marshal([]string{"--config-dir", filepath.Join(root, "config"), "--json", "pipe", target})
	executable, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestFSWatchCLIHelper$")
	command.Env = append(os.Environ(), "OC_TEST_WATCH_CLI_ARGS="+string(raw))
	command.Stdin = strings.NewReader("complete")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, output)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "complete" {
		t.Fatalf("%v %q", err, data)
	}
}
