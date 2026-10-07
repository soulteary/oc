package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/notify"
	"github.com/soulteary/mc/pkg/probe"
)

func TestFSWatchWindowsLoss(t *testing.T) {
	wo := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error, 1), DoneChan: make(chan struct{})}
	input := make(chan notify.EventInfo)
	done := make(chan struct{})
	go forwardFSWatchEvents(context.Background(), wo, input, func() { close(done) })
	input <- fsWatchTestEvent{path: "root", event: notify.WindowsEventsLost}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Windows loss did not terminate watcher")
	}
	err := <-wo.Errors()
	if err == nil {
		t.Fatal("missing native loss error")
	}
	if code, _ := classifyClientError(err.ToGoError()); code != "WatchEventsLost" {
		t.Fatalf("unexpected error code %s", code)
	}
	awaitFSWatchClosed(t, wo)
}
