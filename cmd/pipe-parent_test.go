//go:build !windows

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPipeHelperExitsAfterParentKilled(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	raw, _ := json.Marshal([]string{"--config-dir", filepath.Join(root, "config"), "pipe", filepath.Join(root, "out")})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parent := exec.CommandContext(ctx, executable, "-test.run=^TestFSWatchCLIHelper$")
	parent.Env = append(os.Environ(), "OC_TEST_WATCH_CLI_ARGS="+string(raw))
	stdin, err := parent.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err = parent.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { parent.Process.Kill(); parent.Wait() }()
	if _, err = stdin.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	childPID := 0
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); {
		data, err := exec.Command("ps", "-ax", "-o", "pid=,ppid=,stat=,args=").Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[1] == strconv.Itoa(parent.Process.Pid) && strings.Contains(line, pipeInputArg) {
				childPID, _ = strconv.Atoi(fields[0])
			}
		}
		if childPID != 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatal("input helper was not found")
	}
	child, err := os.FindProcess(childPID)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Kill()
	if err = parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	parent.Wait()
	// Keep upstream stdin open: its EOF must not be what releases the helper.
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); {
		data, err := exec.Command("ps", "-p", strconv.Itoa(childPID), "-o", "stat=").Output()
		if err != nil || strings.TrimSpace(string(data)) == "" || strings.HasPrefix(strings.TrimSpace(string(data)), "Z") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("orphan helper remained blocked on upstream input")
}
