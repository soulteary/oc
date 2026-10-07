package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/soulteary/otterio/pkg/madmin"
)

var errConsoleStreamClosed = errors.New("console log stream closed unexpectedly")

type adminStreamErrorsKey struct{}
type adminLogStream struct {
	ioCtx            context.Context
	cancelIO, cancel context.CancelFunc
	failures         chan error
	records          chan madmin.LogInfo
	once, closed     sync.Once
	started          atomic.Bool
	readerDone       chan struct{}
}

func adminStreamContext(ctx context.Context) (context.Context, *adminLogStream) {
	ioCtx, cancelIO := context.WithCancel(ctx)
	requestCtx, cancel := context.WithCancel(ctx)
	stream := &adminLogStream{ioCtx: ioCtx, cancelIO: cancelIO, cancel: cancel, failures: make(chan error, 1), records: make(chan madmin.LogInfo, 1), readerDone: make(chan struct{})}
	return context.WithValue(requestCtx, adminStreamErrorsKey{}, stream), stream
}
func (s *adminLogStream) fail(err error) { s.once.Do(func() { s.failures <- err; s.cancel() }) }
func (s *adminLogStream) finish()        { s.closed.Do(func() { close(s.records); close(s.readerDone) }) }

// The SDK signs and sends the request, but OC owns the response decoder. Its
// network context is independent of the SDK retry context: stopping the SDK's
// decoder/retry loop must not discard valid buffered log records.
func (s *adminLogStream) decode(body io.ReadCloser) io.ReadCloser {
	if !s.started.CompareAndSwap(false, true) {
		body.Close()
		return io.NopCloser(strings.NewReader(""))
	}
	go func() {
		defer s.finish()
		var closed sync.Once
		closeBody := func() { closed.Do(func() { _ = body.Close() }) }
		defer closeBody()
		stop := context.AfterFunc(s.ioCtx, closeBody)
		defer stop()
		decoder := json.NewDecoder(body)
		for {
			var info madmin.LogInfo
			if err := decoder.Decode(&info); err != nil {
				if s.ioCtx.Err() == nil {
					if errors.Is(err, io.EOF) {
						s.fail(errConsoleStreamClosed)
					} else {
						s.fail(fmt.Errorf("decode console log stream: %w", err))
					}
				}
				return
			}
			select {
			case s.records <- info:
			case <-s.ioCtx.Done():
				return
			}
		}
	}()
	// GetLogs sees EOF, then exits on its canceled retry context. Its producer
	// is joined by the command while the separate OC reader continues streaming.
	s.cancel()
	return io.NopCloser(strings.NewReader(""))
}

func consoleLogs(ctx context.Context, client *madmin.AdminClient, node string, limit int, kind string) (*adminLogStream, func()) {
	requestCtx, stream := adminStreamContext(ctx)
	sdkLogs := client.GetLogs(requestCtx, node, limit, kind)
	sdkDone := make(chan struct{})
	go func() {
		defer close(sdkDone)
		for info := range sdkLogs {
			if info.Err != nil {
				stream.fail(info.Err)
			}
		}
		if !stream.started.Load() {
			if ctx.Err() == nil {
				stream.fail(errConsoleStreamClosed)
			}
			stream.finish()
		}
	}()
	cleanup := func() { stream.cancelIO(); stream.cancel(); <-sdkDone; <-stream.readerDone }
	return stream, cleanup
}
