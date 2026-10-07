package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/soulteary/mc/pkg/probe"
	"github.com/urfave/cli/v3"
)

func TestFindWatchUnexpectedEndFails(t *testing.T) {
	watch := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error)}
	close(watch.EventInfoChan)
	close(watch.ErrorChan)
	err := watchFind(context.Background(), &findContext{watch: true, clnt: endedWatchClient{watch: watch}})
	code, ok := err.(cli.ExitCoder)
	if !ok || code.ExitCode() != globalErrorExitStatus {
		t.Fatalf("unexpected end returned %v", err)
	}
}

func TestFindWatchContextTermination(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		watch := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error)}
		ctx, cancel := context.WithCancel(context.Background())
		if deadline {
			cancel()
			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
		}
		if !deadline {
			cancel()
		}
		err := watchFind(ctx, &findContext{watch: true, clnt: stalledWatchClient{watch: watch}})
		cancel()
		if deadline && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline returned %v", err)
		}
		if !deadline && err != nil {
			t.Fatalf("user cancellation returned %v", err)
		}
	}
}
