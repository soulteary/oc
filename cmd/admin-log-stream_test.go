package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConsoleStreamOwnsDecodeAndTermination(t *testing.T) {
	for _, payload := range []string{"", `{"ConsoleMsg":"last-record"}`, `{"ConsoleMsg":"last-record"}` + "\n{", `[]`} {
		t.Run(fmt.Sprintf("payload=%q", payload), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				fmt.Fprint(w, payload)
			}))
			defer server.Close()
			api, err := NewAdminFactory()(&Config{HostURL: server.URL, AccessKey: "test", SecretKey: "testtest"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			stream, cleanup := consoleLogs(ctx, api, "", 0, "all")
			defer cleanup()
			var records []string
			for info := range stream.records {
				records = append(records, info.ConsoleMsg)
			}
			if ctx.Err() != nil {
				t.Fatal("stream did not terminate")
			}
			if strings.Contains(payload, "last-record") && (len(records) != 1 || records[0] != "last-record") {
				t.Fatalf("valid record lost: %v", records)
			}
			select {
			case err := <-stream.failures:
				if err == nil {
					t.Fatal("missing error")
				}
			default:
				t.Fatal("termination swallowed")
			}
			if requests.Load() != 1 {
				t.Fatalf("EOF/invalid data triggered reconnect loop: %d", requests.Load())
			}
		})
	}
}

type countedLogBody struct {
	io.ReadCloser
	closes atomic.Int32
}

func (b *countedLogBody) Close() error { b.closes.Add(1); return b.ReadCloser.Close() }

func TestConsoleStreamClosesBodyOnCancellation(t *testing.T) {
	for _, blockedConsumer := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		_, stream := adminStreamContext(ctx)
		reader, writer := io.Pipe()
		body := &countedLogBody{ReadCloser: reader}
		fake := stream.decode(body)
		fake.Close()
		writeDone := make(chan struct{})
		go func() {
			defer close(writeDone)
			defer writer.Close()
			if blockedConsumer {
				for i := 0; i < 3; i++ {
					if _, err := fmt.Fprintln(writer, `{"ConsoleMsg":"message"}`); err != nil {
						return
					}
				}
			}
			<-ctx.Done()
		}()
		cancel()
		select {
		case <-stream.readerDone:
		case <-time.After(time.Second):
			t.Fatal("reader leaked after cancellation")
		}
		<-writeDone
		if body.closes.Load() != 1 {
			t.Fatalf("body closed %d times", body.closes.Load())
		}
		select {
		case err := <-stream.failures:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		default:
		}
	}
}

func TestConsoleLiveStreamSurvivesSDKCancellation(t *testing.T) {
	messages := make(chan string, 2)
	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(released)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for {
			select {
			case message := <-messages:
				fmt.Fprintf(w, "{\"ConsoleMsg\":%q}\n", message)
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer server.Close()
	api, err := NewAdminFactory()(&Config{HostURL: server.URL, AccessKey: "test", SecretKey: "testtest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, cleanup := consoleLogs(ctx, api, "", 0, "all")
	defer cleanup()
	for _, message := range []string{"first", "second"} {
		messages <- message
		select {
		case info, ok := <-stream.records:
			if !ok || info.ConsoleMsg != message {
				t.Fatalf("live stream lost record: %v %v", ok, info)
			}
		case <-time.After(time.Second):
			t.Fatal("live stream stalled after SDK cancellation")
		}
	}
	select {
	case <-released:
		t.Fatal("SDK stop closed the live response")
	default:
	}
	cancel()
	cleanup()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not release HTTP response")
	}
}
