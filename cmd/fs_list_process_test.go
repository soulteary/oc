package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestFSListIsolatedMatchesLocal(t *testing.T) {
	client := newCancellationFSClient(t)
	if runtime.GOOS == "linux" {
		if err := os.WriteFile(filepath.Join(client.PathURL.Path, "invalid-\xff"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, opts := range []ListOptions{{}, {Recursive: true}, {Recursive: true, ShowDir: DirFirst}, {Recursive: true, ShowDir: DirLast}} {
		collect := func(ch <-chan *ClientContent) []ClientContent {
			var entries []ClientContent
			for c := range ch {
				if c.Err != nil {
					t.Fatal(c.Err)
				}
				entries = append(entries, *c)
			}
			return entries
		}
		expected := collect(client.listInProcess(context.Background(), opts))
		actual := collect(client.List(context.Background(), opts))
		if !reflect.DeepEqual(expected, actual) {
			t.Fatalf("listing changed for %+v: expected %#v, got %#v", opts, expected, actual)
		}
	}
}

func TestFSListReadPermissionError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0700)
	if _, err := os.ReadDir(root); err == nil {
		t.Skip("user can bypass directory permissions")
	}
	client, err := fsNew(root + string(os.PathSeparator))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for c := range client.List(context.Background(), ListOptions{}) {
		count++
		if c.Err == nil || !errors.Is(c.Err.ToGoError(), os.ErrPermission) {
			t.Fatalf("expected permission error, got %#v", c)
		}
	}
	if count != 1 {
		t.Fatalf("expected one error, got %d entries", count)
	}
}

func TestFSListProcessCancellationAndEarlyExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper")
	}
	t.Run("blocked filesystem stand-in", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		command := exec.Command("/bin/sh", "-c", "exec sleep 60")
		done := make(chan error, 1)
		go func() { done <- runFSListProcess(ctx, command, fsListRequest{}, make(chan *ClientContent)) }()
		time.AfterFunc(100*time.Millisecond, cancel)
		select {
		case <-done:
			if command.ProcessState == nil {
				t.Fatal("helper was not reaped")
			}
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatal("blocked helper prevented cancellation")
		}
	})
	t.Run("early EOF", func(t *testing.T) {
		command := exec.Command("/bin/sh", "-c", "exit 0")
		if err := runFSListProcess(context.Background(), command, fsListRequest{}, make(chan *ClientContent)); err == nil {
			t.Fatal("early EOF reported success")
		}
	})
}

func TestReadDirErrorClosesFile(t *testing.T) {
	descriptors := "/dev/fd"
	if runtime.GOOS == "linux" {
		descriptors = "/proc/self/fd"
	}
	before, err := os.ReadDir(descriptors)
	if err != nil {
		t.Skip("descriptor inventory unavailable")
	}
	path := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		if _, err := readDir(path); err == nil {
			t.Fatal("reading regular file as directory succeeded")
		}
	}
	after, err := os.ReadDir(descriptors)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > len(before) {
		t.Fatalf("directory errors leaked descriptors: before %d, after %d", len(before), len(after))
	}
}
