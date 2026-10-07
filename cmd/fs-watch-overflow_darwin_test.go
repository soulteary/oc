//go:build darwin && !kqueue && cgo

package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/notify"
	"github.com/soulteary/mc/pkg/probe"
)

type nativeLossTestEvent struct {
	fsWatchTestEvent
	flags uint32
}

func (e nativeLossTestEvent) Sys() interface{} { return &notify.FSEvent{Path: e.path, Flags: e.flags} }

func TestFSWatchNativeLoss(t *testing.T) {
	for _, flag := range []notify.Event{notify.FSEventsMustScanSubDirs, notify.FSEventsUserDropped, notify.FSEventsKernelDropped, notify.FSEventsRootChanged} {
		wo := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error, 1), DoneChan: make(chan struct{})}
		input := make(chan notify.EventInfo)
		done := make(chan struct{})
		go forwardFSWatchEvents(context.Background(), wo, input, func() { close(done) })
		input <- nativeLossTestEvent{fsWatchTestEvent: fsWatchTestEvent{path: "root", event: flag}, flags: uint32(flag)}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("native loss ignored")
		}
		err := <-wo.Errors()
		if err == nil {
			t.Fatal("missing native loss error")
		}
		code, _ := classifyClientError(err.ToGoError())
		if code != "WatchEventsLost" {
			t.Fatalf("unexpected code %s", code)
		}
		awaitFSWatchClosed(t, wo)
	}
}
