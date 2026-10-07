//go:build !windows

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestPipeAndMirrorSignalCleanup(t *testing.T) {
	for _, kind := range []string{"pipe", "mirror"} {
		t.Run(kind, func(t *testing.T) {
			for _, sig := range []struct {
				signal os.Signal
				code   int
			}{{os.Interrupt, globalCancelExitStatus}, {syscall.SIGTERM, globalTerminatExitStatus}} {
				root := t.TempDir()
				target := filepath.Join(root, "target")
				args := []string{"--config-dir", filepath.Join(root, "config"), "--json", kind}
				if kind == "mirror" {
					source := filepath.Join(root, "source")
					os.Mkdir(source, 0700)
					os.Mkdir(target, 0700)
					os.WriteFile(filepath.Join(source, "file"), []byte("test"), 0600)
					args = append(args, "--watch", "--watch-rescan-interval", "1s", source, target)
				} else {
					args = append(args, target)
				}
				raw, _ := json.Marshal(args)
				executable, _ := os.Executable()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, executable, "-test.run=^TestFSWatchCLIHelper$")
				command.Env = append(os.Environ(), "OC_TEST_WATCH_CLI_ARGS="+string(raw))
				var output bytes.Buffer
				command.Stdout = &output
				command.Stderr = &output
				stdin, err := command.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				defer stdin.Close()
				if err = command.Start(); err != nil {
					t.Fatal(err)
				}
				defer command.Process.Kill()
				if kind == "pipe" {
					stdin.Write([]byte("partial"))
				}
				ready := false
				for end := time.Now().Add(8 * time.Second); time.Now().Before(end); {
					if kind == "pipe" {
						stages, _ := filepath.Glob(filepath.Join(root, ".oc-part-*"))
						ready = len(stages) > 0
					} else {
						_, err := os.Stat(filepath.Join(target, "file"))
						ready = err == nil
					}
					if ready {
						break
					}
					time.Sleep(20 * time.Millisecond)
				}
				if !ready {
					t.Fatal("command never became ready")
				}
				if err = command.Process.Signal(sig.signal); err != nil {
					t.Fatal(err)
				}
				err = command.Wait()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != sig.code {
					t.Fatalf("want exit %d, got %v: %s", sig.code, err, output.String())
				}
				if kind == "pipe" {
					stages, _ := filepath.Glob(filepath.Join(root, ".oc-part-*"))
					if len(stages) > 0 {
						t.Fatalf("staging leaked: %v", stages)
					}
					if _, err := os.Stat(target); !os.IsNotExist(err) {
						t.Fatal("canceled pipe committed target")
					}
				}
			}
		})
	}
}
