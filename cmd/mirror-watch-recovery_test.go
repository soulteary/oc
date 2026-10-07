package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/soulteary/mc/pkg/probe"
)

func TestMirrorWatchRecoveryTriggers(t *testing.T) {
	for _, trigger := range []string{"overflow", "periodic"} {
		t.Run(trigger, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			watcher := NewWatcher(time.Now())
			watcher.localFilesystem = true
			job := &mirrorJob{watcher: watcher, statusCh: make(chan URLs, 1), stopCh: make(chan struct{})}
			if trigger == "periodic" {
				job.opts.watchRescanInterval = time.Millisecond
			}
			stopped := make(chan struct{})
			done := make(chan struct{})
			go func() { defer close(done); job.watchMirror(ctx, func() { close(stopped) }) }()
			if trigger == "overflow" {
				watcher.ErrorChan <- probe.NewError(fsWatchOverflow{})
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("watch recovery did not stop current run")
			}
			select {
			case <-stopped:
			default:
				t.Fatal("parallel manager not stopped")
			}
			if trigger == "overflow" && !job.rescanRequired || trigger == "periodic" && (!job.verifyContents || job.rescanRequired) {
				t.Fatal("next run will not reconcile")
			}
			if ctx.Err() != nil {
				t.Fatal("watch recovery canceled the parent context")
			}
			if trigger == "overflow" {
				select {
				case result := <-job.statusCh:
					if result.Error == nil {
						t.Fatal("overflow error lost")
					}
				default:
					t.Fatal("overflow not reported")
				}
			}
		})
	}
}

func TestMirrorReconcileEqualMetadata(t *testing.T) {
	previous := loadMcConfig
	loadMcConfig = func() (*configV10, *probe.Error) { return newConfigV10(), nil }
	defer func() { loadMcConfig = previous }()
	source, target := t.TempDir(), t.TempDir()
	sourcePath, targetPath := filepath.Join(source, "file"), filepath.Join(target, "file")
	if err := os.WriteFile(sourcePath, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Unix(1700000000, 0)
	for _, path := range []string{sourcePath, targetPath} {
		if err := os.Chtimes(path, timestamp, timestamp); err != nil {
			t.Fatal(err)
		}
	}
	for _, reconcile := range []bool{false, true} {
		count := 0
		for result := range prepareMirrorURLs(context.Background(), source, target, mirrorOptions{isOverwrite: true, reconcile: reconcile}) {
			if result.Error != nil {
				t.Fatal(result.Error)
			}
			if result.SourceContent != nil {
				count++
			}
		}
		expected := 0
		if reconcile {
			expected = 1
		}
		if count != expected {
			t.Fatalf("reconcile=%v: queued %d, want %d", reconcile, count, expected)
		}
	}
}

func TestFSWatchCLIHelper(t *testing.T) {
	raw := os.Getenv("OC_TEST_WATCH_CLI_ARGS")
	if raw == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		os.Exit(2)
	}
	Main(append([]string{"oc"}, args...))
	os.Exit(0)
}

func TestMirrorPeriodicRecoveryCLI(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	for _, dir := range []string{source, target} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	sourcePath, targetPath := filepath.Join(source, "file"), filepath.Join(target, "file")
	if err := os.WriteFile(sourcePath, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal([]string{"--config-dir", filepath.Join(root, "config"), "--json", "mirror", "--watch", "--overwrite", "--remove", "--watch-rescan-interval", "1s", source, target})
	command := exec.Command(executable, "-test.run=^TestFSWatchCLIHelper$")
	command.Env = append(os.Environ(), "OC_TEST_WATCH_CLI_ARGS="+string(args))
	log, err := os.Create(filepath.Join(root, "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	defer func() {
		_ = command.Process.Kill()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("mirror child did not exit")
		}
	}()
	waitFor := func(label string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for !condition() {
			select {
			case err := <-finished:
				finished <- err
				data, _ := os.ReadFile(log.Name())
				t.Fatalf("%s: mirror exited: %v\n%s", label, err, data)
			default:
			}
			if time.Now().After(deadline) {
				data, _ := os.ReadFile(log.Name())
				t.Fatalf("%s timed out\n%s", label, data)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitFor("initial copy", func() bool { data, _ := os.ReadFile(targetPath); return string(data) == "new" })
	// Corrupt the target only. The source emits no notification; a watcher-only
	// implementation cannot notice this. Size and timestamps also match.
	if err := os.WriteFile(targetPath, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(targetPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(target, "orphan")
	if err := os.WriteFile(orphan, []byte("extra"), 0600); err != nil {
		t.Fatal(err)
	}
	waitFor("periodic content repair and deletion", func() bool {
		data, _ := os.ReadFile(targetPath)
		_, err := os.Stat(orphan)
		return string(data) == "new" && os.IsNotExist(err)
	})
	before, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	// One interval plus the bounded retry jitter must pass without a rewrite.
	time.Sleep(3500 * time.Millisecond)
	after, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || !os.SameFile(before, after) {
		t.Fatal("periodic verification rewrote unchanged target")
	}
}
