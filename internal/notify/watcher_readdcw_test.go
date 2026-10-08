//go:build windows

package notify

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestWindowsRegistrationFailureClosesHandle(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	port, err := syscall.CreateIoCompletionPort(syscall.InvalidHandle, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(port)
	for _, tc := range []struct {
		name string
		path string
		port syscall.Handle
	}{
		{"completion-port", dir, syscall.InvalidHandle},
		{"first-read", file, port},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, err := syscall.UTF16FromString(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			g := &grip{handle: syscall.InvalidHandle, filter: uint32(Write), pathw: path, ovlapped: &overlappedEx{}}
			g.ovlapped.parent = g
			if err := g.register(tc.port); err == nil {
				syscall.CloseHandle(g.handle)
				t.Fatal("invalid watch registration succeeded")
			}
			if g.handle != syscall.InvalidHandle {
				syscall.CloseHandle(g.handle)
				t.Fatal("failed watch registration retained a directory handle")
			}
		})
	}
}

func TestWindowsWriteIncludesLastWrite(t *testing.T) {
	if encode(uint32(Write))&uint32(FileNotifyChangeLastWrite) == 0 {
		t.Fatal("same-size writes are not included in the Write filter")
	}
}

func TestWindowsWatchSameSizeWrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	out := make(chan EventInfo, 10)
	r := newWatcher(out).(*readdcw)
	defer r.Close()
	if err := r.Watch(dir, Write); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("new")); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	select {
	case event := <-out:
		if event.Event() != Write || event.Path() != file {
			t.Fatalf("unexpected write event: %v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("same-size write was not delivered")
	}
}

func TestWindowsCompletionReportsLoss(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    uint32
		err  error
	}{
		{"overflow", 0, nil},
		{"truncated-header", 11, nil},
		{"oversized-buffer", readBufferSize + 1, nil},
		{"read-error", 0, syscall.ERROR_ACCESS_DENIED},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := make(chan EventInfo, 1)
			r := &readdcw{c: out}
			path, _ := syscall.UTF16FromString(`C:\watched`)
			r.completion(tc.n, tc.err, &overlappedEx{parent: &grip{pathw: path}})
			select {
			case event := <-out:
				if event.Event() != WindowsEventsLost || event.Path() != `C:\watched` {
					t.Fatalf("unexpected loss event: %v", event)
				}
			default:
				t.Fatal("lost notifications were silently discarded")
			}
		})
	}
	out := make(chan EventInfo, 1)
	r := &readdcw{c: out}
	r.completion(0, syscall.ERROR_OPERATION_ABORTED, &overlappedEx{parent: &grip{}})
	if len(out) != 0 {
		t.Fatal("intentional cancellation reported event loss")
	}
	r.completion(0, nil, &overlappedEx{parent: &grip{handle: syscall.InvalidHandle}})
	if len(out) != 0 {
		t.Fatal("successful completion of a closed handle reported event loss")
	}
}

func TestWindowsClosePublishesInvalidHandleBeforeCompletion(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"close-error", syscall.ERROR_ACCESS_DENIED},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := make(chan EventInfo, 1)
			r := &readdcw{c: out}
			path, _ := syscall.UTF16FromString(`C:\watched`)
			g := &grip{handle: 123, pathw: path}
			over := &overlappedEx{parent: g}
			calls := 0
			closeHandle := func(handle syscall.Handle) error {
				calls++
				if handle != 123 {
					t.Fatalf("closed handle %v, want original handle 123", handle)
				}
				// Simulate cancellation completing while CloseHandle is still
				// executing, before the former close-then-CAS could invalidate it.
				r.completion(0, nil, over)
				if len(out) != 0 {
					t.Fatal("in-progress handle close reported event loss")
				}
				if syscall.Handle(atomic.LoadUintptr((*uintptr)(&g.handle))) != syscall.InvalidHandle {
					t.Fatal("kernel close started before the grip was retired")
				}
				return tc.err
			}
			if err := g.closeHandle(closeHandle); err != tc.err {
				t.Fatalf("close error %v, want %v", err, tc.err)
			}
			if err := g.closeHandle(closeHandle); err != nil || calls != 1 {
				t.Fatalf("retired handle closed again: calls=%d, error=%v", calls, err)
			}
		})
	}
}

func TestWindowsLostDispatchIncludesRelatedSubscriptions(t *testing.T) {
	tree := &recursiveTree{root: root{nd: newnode("")}}
	base := filepath.Join(t.TempDir(), "watched")
	parent := tree.root.Add(filepath.Dir(base))
	self := tree.root.Add(base)
	child := tree.root.Add(filepath.Join(base, "child"))
	sibling := tree.root.Add(filepath.Join(filepath.Dir(base), "sibling"))
	outputs := make([]chan EventInfo, 4)
	for i := range outputs {
		outputs[i] = make(chan EventInfo, 1)
	}
	parent.Watch.Add(outputs[0], Create|recursive)
	self.Watch.Add(outputs[1], Remove)
	child.Watch.Add(outputs[2], Write)
	sibling.Watch.Add(outputs[3], Create)
	path, _ := syscall.UTF16FromString(base)
	tree.dispatchLost(&event{pathw: path, e: WindowsEventsLost})
	for i, output := range outputs {
		want := 1
		if i == 3 {
			want = 0
		}
		if len(output) != want {
			t.Fatalf("subscription %d got %d loss events, want %d", i, len(output), want)
		}
	}
}

func TestWindowsIdleStopRewatchReleasesHandles(t *testing.T) {
	path := t.TempDir()
	out := make(chan EventInfo, 100)
	r := newWatcher(out).(*readdcw)
	defer r.Close()
	for i := 0; i < 20; i++ {
		if err := r.Watch(path, Create); err != nil {
			t.Fatal(err)
		}
		r.Lock()
		old := r.m[path]
		r.Unlock()
		if err := r.Unwatch(path); err != nil {
			t.Fatal(err)
		}
		for _, grip := range old.digrip {
			if grip != nil && syscall.Handle(atomic.LoadUintptr((*uintptr)(&grip.handle))) != syscall.InvalidHandle {
				t.Fatal("idle Stop kept a directory handle open")
			}
		}
	}
	if err := r.Watch(path, Create); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(path, "after-rewatch")
	if err := os.WriteFile(target, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-out:
		if event.Event() != Create || event.Path() != target {
			t.Fatalf("unexpected event after rewatch: %v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("new watch was removed by an old cancellation completion")
	}
	if err := r.Unwatch(path); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsRetiredCompletionKeepsReplacementWatch(t *testing.T) {
	path, _ := syscall.UTF16FromString(`C:\watched`)
	old := &watched{filter: stateUnwatch, count: 2, pathw: path}
	replacement := &watched{count: 1, pathw: path}
	r := &readdcw{m: map[string]*watched{`C:\watched`: replacement}, retired: map[*watched]struct{}{old: {}}}
	r.drained = sync.NewCond(&r.Mutex)
	over := &overlappedEx{parent: &grip{parent: old, pathw: path}}
	r.Lock()
	defer r.Unlock()
	if err := r.loopstateLocked(over, false); err != nil {
		t.Fatal(err)
	}
	if _, retained := r.retired[old]; !retained {
		t.Fatal("notification memory was released before all cancellations completed")
	}
	if err := r.loopstateLocked(over, false); err != nil {
		t.Fatal(err)
	}
	if len(r.retired) != 0 || r.m[`C:\watched`] != replacement {
		t.Fatal("retired completion leaked memory or removed a replacement watch")
	}
}
