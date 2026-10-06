package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/cli"
	"github.com/rjeczalik/notify"
	"github.com/soulteary/mc/pkg/probe"
)

type fsWatchTestEvent struct {
	path  string
	event notify.Event
}

func (e fsWatchTestEvent) Path() string        { return e.path }
func (e fsWatchTestEvent) Event() notify.Event { return e.event }
func (e fsWatchTestEvent) Sys() interface{}    { return nil }

func awaitFSWatchClosed(t *testing.T, wo *WatchObject) {
	t.Helper()
	select {
	case _, ok := <-wo.Events():
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(time.Second):
		t.Fatal("event channel did not close")
	}
	select {
	case _, ok := <-wo.Errors():
		if ok {
			t.Fatal("unexpected error")
		}
	case <-time.After(time.Second):
		t.Fatal("error channel did not close")
	}
}

func TestFSWatchForwarderCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, shutdown := range []string{"context", "done"} {
		for _, state := range []string{"idle", "event", "error", "closed"} {
			t.Run(shutdown+"/"+state, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				wo := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error), DoneChan: make(chan struct{})}
				input := make(chan notify.EventInfo)
				stopped := make(chan struct{})
				go forwardFSWatchEvents(ctx, wo, input, func() { close(stopped) })
				if state == "event" || state == "error" {
					eventPath := path
					// A path component that exceeds supported filename lengths makes Stat
					// fail without depending on permissions or a race with file removal.
					if state == "error" {
						eventPath = filepath.Join(filepath.Dir(path), strings.Repeat("x", 5000))
					}
					select {
					case input <- fsWatchTestEvent{eventPath, EventTypePut[0]}:
					case <-time.After(time.Second):
						t.Fatal("input blocked")
					}
				}
				if state == "closed" {
					close(input)
				} else if shutdown == "context" {
					cancel()
				} else {
					close(wo.DoneChan)
				}
				select {
				case <-stopped:
				case <-time.After(time.Second):
					t.Fatal("watcher did not stop with a blocked consumer")
				}
				awaitFSWatchClosed(t, wo)
			})
		}
	}
}

func TestFSWatchNativeCancellation(t *testing.T) {
	for _, shutdown := range []string{"context", "done"} {
		t.Run(shutdown, func(t *testing.T) {
			client, err := fsNew(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wo, err := client.Watch(ctx, WatchOptions{Events: []string{"put"}})
			if err != nil {
				t.Fatal(err)
			}
			if shutdown == "context" {
				cancel()
			} else {
				close(wo.DoneChan)
			}
			awaitFSWatchClosed(t, wo)
		})
	}
}

func TestFSWatchCancelWhileSending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		wo := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error), DoneChan: make(chan struct{})}
		input := make(chan notify.EventInfo)
		stopped := make(chan struct{})
		consumed := make(chan struct{})
		go forwardFSWatchEvents(ctx, wo, input, func() { close(stopped) })
		go func() {
			defer close(consumed)
			for range wo.Events() {
			}
		}()
		input <- fsWatchTestEvent{path, EventTypePut[0]}
		go cancel()
		close(wo.DoneChan)
		select {
		case <-consumed:
		case <-time.After(time.Second):
			t.Fatal("consumer did not finish")
		}
		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Fatal("watcher did not stop")
		}
		awaitFSWatchClosed(t, wo)
	}
}

func TestFSWatchQueueSaturation(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint(blocked), func(t *testing.T) {
			input := make(chan notify.EventInfo, 4)
			wo := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error, 1), DoneChan: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			stat := func(ctx context.Context, _ string) (fsWatchFileState, error) {
				close(entered)
				<-ctx.Done()
				return fsWatchFileState{}, ctx.Err()
			}
			if !blocked {
				for i := 0; i < cap(input); i++ {
					input <- fsWatchTestEvent{"object", EventTypePut[0]}
				}
			}
			stopped := make(chan struct{})
			go forwardFSWatchEventsWithStat(ctx, wo, input, func() { close(stopped) }, stat)
			if blocked {
				input <- fsWatchTestEvent{"object", EventTypePut[0]}
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("metadata lookup did not start")
				}
				for i := 0; i < cap(input); i++ {
					input <- fsWatchTestEvent{"object", EventTypePut[0]}
				}
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("saturation did not stop watcher")
			}
			err, ok := <-wo.Errors()
			if !ok || err == nil {
				t.Fatal("saturation was silent")
			}
			var overflow fsWatchOverflow
			if !errors.As(err.ToGoError(), &overflow) {
				t.Fatalf("wrong error: %v", err)
			}
			code, category := classifyClientError(err.ToGoError())
			if code != "WatchQueueSaturated" || category != "watch" {
				t.Fatalf("classification: %s/%s", code, category)
			}
			awaitFSWatchClosed(t, wo)
		})
	}
}

func TestFSWatchNativeBurst(t *testing.T) {
	// FSEvents may exclude OS temporary directories; use a private directory
	// in the checkout and resolve symlinks before registering the watcher.
	dir, createErr := os.MkdirTemp(".", ".oc-watch-test-")
	if createErr != nil {
		t.Fatal(createErr)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir, createErr = filepath.Abs(dir)
	if createErr != nil {
		t.Fatal(createErr)
	}
	dir, createErr = filepath.EvalSymlinks(dir)
	if createErr != nil {
		t.Fatal(createErr)
	}
	client, err := fsNew(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wo, err := client.Watch(ctx, WatchOptions{Events: []string{"put"}})
	if err != nil {
		t.Fatal(err)
	}
	// Registration may return before an asynchronous backend is ready. Wait
	// for a real notification instead of assuming the first writes are seen.
	ready := false
	deadline := time.Now().Add(10 * time.Second)
	for !ready {
		if time.Now().After(deadline) {
			t.Fatal("native watcher did not deliver a real event")
		}
		if err := os.WriteFile(filepath.Join(dir, "ready"), []byte("ready"), 0600); err != nil {
			t.Fatal(err)
		}
		select {
		case <-wo.Events():
			ready = true
		case err := <-wo.Errors():
			t.Fatal(err)
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Stop reading output while real file events exceed the queue budget.
	for i := 0; i < 3000; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%d", i)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-wo.Errors():
		if err == nil {
			t.Fatal("missing saturation error")
		}
		var overflow fsWatchOverflow
		if !errors.As(err.ToGoError(), &overflow) {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("native queue saturation was not reported")
	}
	awaitFSWatchClosed(t, wo)
}

func TestFSWatchStatWorkerReuse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := &fsWatchStatWorker{}
	defer worker.close()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := worker.stat(ctx, path)
	if err != nil || first.Size != 4 || first.Directory {
		t.Fatalf("stat: %+v %v", first, err)
	}
	process := worker.command.Process
	second, err := worker.stat(ctx, filepath.Dir(path))
	if err != nil || !second.Directory {
		t.Fatalf("directory: %+v %v", second, err)
	}
	if worker.command.Process != process {
		t.Fatal("metadata helper was not reused")
	}
	_, err = worker.stat(ctx, path+"missing")
	if !os.IsNotExist(err) {
		t.Fatalf("missing path: %v", err)
	}
	worker.close()
	if worker.command != nil {
		t.Fatal("metadata helper not reaped")
	}
}

func TestFSWatchBlockedStatHelper(t *testing.T) {
	if os.Getenv("OC_TEST_BLOCKING_STAT") != "1" {
		return
	}
	var path string
	if err := json.NewDecoder(os.Stdin).Decode(&path); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Getenv("OC_TEST_STAT_SIGNAL"), []byte("ready"), 0600); err != nil {
		os.Exit(3)
	}
	time.Sleep(time.Hour)
	os.Exit(0)
}

func TestFSWatchBlockedStatCancellation(t *testing.T) {
	for _, shutdown := range []string{"context", "done"} {
		t.Run(shutdown, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			signal := filepath.Join(t.TempDir(), "ready")
			command := exec.Command(executable, "-test.run=^TestFSWatchBlockedStatHelper$")
			command.Env = append(os.Environ(), "OC_TEST_BLOCKING_STAT=1", "OC_TEST_STAT_SIGNAL="+signal)
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			worker := &fsWatchStatWorker{command: command, input: input, output: output, encoder: json.NewEncoder(input), decoder: json.NewDecoder(output)}
			defer worker.close()
			wo := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error, 1), DoneChan: make(chan struct{})}
			events := make(chan notify.EventInfo)
			stopped := make(chan struct{})
			go forwardFSWatchEventsWithStat(ctx, wo, events, func() { worker.close(); close(stopped) }, worker.stat)
			events <- fsWatchTestEvent{"object", EventTypePut[0]}
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(signal); err == nil {
					break
				}
				if time.Now().After(deadline) {
					cancel()
					t.Fatal("helper did not enter blocked lookup")
				}
				time.Sleep(time.Millisecond)
			}
			if shutdown == "context" {
				cancel()
			} else {
				close(wo.DoneChan)
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("blocked helper prevented cancellation")
			}
			awaitFSWatchClosed(t, wo)
			if command.ProcessState == nil {
				t.Fatal("helper was not reaped")
			}
		})
	}
}

func TestWatcherKeepsTerminalError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error, 1)}
	source.ErrorChan <- probe.NewError(fsWatchOverflow{})
	close(source.EventInfoChan)
	close(source.ErrorChan)
	watcher := NewWatcher(time.Now())
	if err := watcher.Join(ctx, closedWatchClient{watch: source}, true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-watcher.Errors():
		if _, ok := err.ToGoError().(fsWatchOverflow); !ok {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("closed events hid terminal error")
	}
	watcher.Wait()
}

type closedWatchClient struct {
	Client
	watch *WatchObject
}

func (c closedWatchClient) Watch(context.Context, WatchOptions) (*WatchObject, *probe.Error) {
	return c.watch, nil
}

func TestWatchCommandsTerminalError(t *testing.T) {
	for _, command := range []string{"watch", "find"} {
		t.Run(command, func(t *testing.T) {
			wo := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error, 1)}
			wo.ErrorChan <- probe.NewError(fsWatchOverflow{})
			close(wo.EventInfoChan)
			close(wo.ErrorChan)
			var err error
			if command == "watch" {
				err = watchNotifications(context.Background(), wo)
			} else {
				err = watchFind(context.Background(), &findContext{watch: true, clnt: closedWatchClient{watch: wo}})
			}
			code, ok := err.(cli.ExitCoder)
			if !ok || code.ExitCode() != globalErrorExitStatus {
				t.Fatalf("terminal error returned success: %v", err)
			}
		})
	}
}

func TestFSWatchAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client, err := fsNew(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wo, err := client.Watch(ctx, WatchOptions{Events: []string{"put"}})
	if wo != nil || err == nil || !errors.Is(err.ToGoError(), context.Canceled) {
		t.Fatalf("canceled watch registered resources: %v %v", wo, err)
	}
}

func TestFSWatchStatHelperParentPipeClosed(t *testing.T) {
	worker := &fsWatchStatWorker{}
	defer worker.close()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.stat(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	command := worker.command
	if err := worker.input.Close(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("helper did not exit cleanly on parent EOF: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		<-exited
		t.Fatal("helper stayed alive after parent pipe closed")
	}
}
