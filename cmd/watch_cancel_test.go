package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
	"github.com/soulteary/otterio-sdk/v7/pkg/notification"
)

type stalledWatchClient struct {
	Client
	watch *WatchObject
}

func (c stalledWatchClient) Watch(ctx context.Context, _ WatchOptions) (*WatchObject, *probe.Error) {
	go func() { <-ctx.Done(); close(c.watch.EventInfoChan); close(c.watch.ErrorChan) }()
	return c.watch, nil
}

func TestWatcherCancellationWithBlockedConsumer(t *testing.T) {
	for _, kind := range []string{"idle", "events", "errors"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error)}
			watcher := NewWatcher(time.Now())
			if err := watcher.Join(ctx, stalledWatchClient{watch: source}, true); err != nil {
				t.Fatal(err)
			}
			// Unbuffered source send confirms the forwarder has received the value.
			if kind == "events" {
				source.EventInfoChan <- []EventInfo{{Path: "test"}}
			}
			if kind == "errors" {
				source.ErrorChan <- probe.NewError(context.DeadlineExceeded)
			}
			cancel()
			done := make(chan struct{})
			go func() { watcher.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("forwarder remained blocked after cancellation")
			}
		})
	}
}

func TestS3WatchCancellationWithBlockedConsumer(t *testing.T) {
	for _, kind := range []string{"idle", "events", "errors", "closed"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := make(chan notification.Info)
			watch := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error)}
			done := make(chan struct{})
			go func() { (&S3Client{}).forwardWatchNotifications(ctx, watch, source); close(done) }()
			if kind == "closed" {
				close(source)
			}
			if kind == "events" || kind == "errors" {
				value := notification.Info{}
				if kind == "errors" {
					value.Err = context.DeadlineExceeded
				}
				select {
				case source <- value:
				case <-time.After(time.Second):
					t.Fatal("notification not received")
				}
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("S3 forwarder did not stop")
			}
			if _, ok := <-watch.Events(); ok {
				t.Fatal("events channel not closed")
			}
			if _, ok := <-watch.Errors(); ok {
				t.Fatal("errors channel not closed")
			}
		})
	}
}

func TestS3CancellationClosesSubscriptionRequest(t *testing.T) {
	ready, released := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(ready)
		<-r.Context().Done()
		close(released)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Region: "us-east-1", Creds: credentials.NewStaticV4("test", "test-secret", "")})
	if err != nil {
		t.Fatal(err)
	}
	notifications := client.ListenBucketNotification(ctx, "test-bucket", "", "", []string{"s3:ObjectCreated:*"})
	watch := &WatchObject{EventInfoChan: make(chan []EventInfo), ErrorChan: make(chan *probe.Error)}
	done := make(chan struct{})
	go func() { (&S3Client{}).forwardWatchNotifications(ctx, watch, notifications); close(done) }()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("subscription never connected")
	}
	cancel()
	for _, completed := range []<-chan struct{}{done, released} {
		select {
		case <-completed:
		case <-time.After(time.Second):
			t.Fatal("subscription resources did not release")
		}
	}
}
