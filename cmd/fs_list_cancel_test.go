package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newCancellationFSClient(t *testing.T) *fsClient {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"file-a", "file-b", "nested/file-c"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	client, err := fsNew(root + string(os.PathSeparator))
	if err != nil {
		t.Fatal(err)
	}
	return client.(*fsClient)
}

func waitFSListClosed(t *testing.T, ch <-chan *ClientContent) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatal("filesystem listing did not close after cancellation")
		}
	}
}

func TestFSListProducersCancelBlockedSend(t *testing.T) {
	for _, mode := range []string{"prefix", "directory", "recursive", "directories-first", "directories-last"} {
		t.Run(mode, func(t *testing.T) {
			client := newCancellationFSClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out := make(chan *ClientContent)
			done := make(chan struct{})
			go func() {
				defer close(done)
				switch mode {
				case "prefix":
					defer close(out)
					client.listPrefixes(ctx, filepath.Join(client.GetURL().Path, "file-"), out)
				case "directory":
					client.listInRoutine(ctx, out, false)
				case "recursive":
					client.listRecursiveInRoutine(ctx, out, false)
				case "directories-first":
					client.listDirOpt(ctx, out, false, false, DirFirst)
				case "directories-last":
					client.listDirOpt(ctx, out, false, false, DirLast)
				}
			}()
			select {
			case _, ok := <-out:
				if !ok {
					t.Fatal("listing produced no entries")
				}
			case <-time.After(time.Second):
				t.Fatal("listing did not start")
			}
			cancel()
			// Do not drain output: the producer must leave a pending send itself.
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("listing producer remained blocked after cancellation")
			}
			waitFSListClosed(t, out)
		})
	}
}

func TestFSListCancellationClosesPublicChannel(t *testing.T) {
	for _, opts := range []ListOptions{{}, {Recursive: true}, {Recursive: true, ShowDir: DirFirst}, {Recursive: true, ShowDir: DirLast}} {
		for _, preCanceled := range []bool{false, true} {
			client := newCancellationFSClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if preCanceled {
				cancel()
			}
			out := client.List(ctx, opts)
			if !preCanceled {
				select {
				case _, ok := <-out:
					if !ok {
						t.Fatal("listing produced no entries")
					}
				case <-time.After(time.Second):
					cancel()
					t.Fatal("listing did not start")
				}
				cancel()
			}
			waitFSListClosed(t, out)
		}
	}
}
