//go:build windows

package notify

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

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
