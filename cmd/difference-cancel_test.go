package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/soulteary/mc/pkg/probe"
)

type cancellationListClient struct {
	Client
	contents []*ClientContent
}

func (c cancellationListClient) List(ctx context.Context, _ ListOptions) <-chan *ClientContent {
	out := make(chan *ClientContent)
	go func() {
		defer close(out)
		for _, content := range c.contents {
			select {
			case out <- content:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func TestDifferenceCancellationWithBlockedConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	content := &ClientContent{URL: *newClientURL("/file"), Size: 3}
	client := cancellationListClient{contents: []*ClientContent{content, content}}
	out := make(chan diffMessage)
	done := make(chan *probe.Error, 1)
	go func() {
		done <- differenceInternal(ctx, client, client, "source", "target", false, true, true, DirNone, out)
	}()
	select {
	case <-out:
	case <-time.After(time.Second):
		t.Fatal("comparison did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err.ToGoError(), context.Canceled) {
			t.Fatalf("cancel returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("comparison blocked after cancellation")
	}
}

type idleComparisonClient struct {
	Client
	started chan<- struct{}
}

func (c idleComparisonClient) List(context.Context, ListOptions) <-chan *ClientContent {
	c.started <- struct{}{}
	return make(chan *ClientContent)
}

func TestDifferenceCancellationWithIdleProducer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 2)
	client := idleComparisonClient{started: started}
	done := make(chan *probe.Error, 1)
	go func() {
		done <- differenceInternal(ctx, client, client, "source", "target", false, true, true, DirNone, make(chan diffMessage))
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("listing did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err.ToGoError(), context.Canceled) {
			t.Fatalf("cancel returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("comparison waited for idle producer after cancellation")
	}
}
