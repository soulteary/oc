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
	"github.com/soulteary/otterio/pkg/trace"
)

func (s *adminTraceStream) ioContext() context.Context { return s.ioCtx }

var errTraceStreamClosed = errors.New("trace stream closed unexpectedly")

type adminTraceStream struct {
	ioCtx            context.Context
	cancelIO, cancel context.CancelFunc
	failures         chan error
	records          chan madmin.ServiceTraceInfo
	once, closed     sync.Once
	started          atomic.Bool
	readerDone       chan struct{}
}

func adminTraceContext(ctx context.Context) (context.Context, *adminTraceStream) {
	ioCtx, cancelIO := context.WithCancel(ctx)
	requestCtx, cancel := context.WithCancel(ctx)
	stream := &adminTraceStream{ioCtx: ioCtx, cancelIO: cancelIO, cancel: cancel, failures: make(chan error, 1), records: make(chan madmin.ServiceTraceInfo, 1), readerDone: make(chan struct{})}
	return context.WithValue(requestCtx, adminStreamErrorsKey{}, stream), stream
}
func (s *adminTraceStream) fail(err error) { s.once.Do(func() { s.failures <- err; s.cancel() }) }
func (s *adminTraceStream) finish()        { s.closed.Do(func() { close(s.records); close(s.readerDone) }) }

// The SDK signs and sends the request, but OC owns the response decoder. Its
// network context is independent of the SDK retry context: stopping the SDK's
// decoder/retry loop must not discard valid buffered trace records.
func (s *adminTraceStream) decode(body io.ReadCloser) io.ReadCloser {
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
			var info trace.Info
			if err := decoder.Decode(&info); err != nil {
				if s.ioCtx.Err() == nil {
					if errors.Is(err, io.EOF) {
						s.fail(errTraceStreamClosed)
					} else {
						s.fail(fmt.Errorf("decode trace stream: %w", err))
					}
				}
				return
			}
			select {
			case s.records <- madmin.ServiceTraceInfo{Trace: info}:
			case <-s.ioCtx.Done():
				return
			}
		}
	}()
	// ServiceTrace sees EOF, then exits on its canceled retry context. Its producer
	// is joined by the command while the separate OC reader continues streaming.
	s.cancel()
	return io.NopCloser(strings.NewReader(""))
}

func traceRecords(ctx context.Context, client *madmin.AdminClient, opts madmin.ServiceTraceOpts) (*adminTraceStream, func()) {
	requestCtx, stream := adminTraceContext(ctx)
	sdkLogs := client.ServiceTrace(requestCtx, opts)
	sdkDone := make(chan struct{})
	go func() {
		defer close(sdkDone)
		for info := range sdkLogs {
			if info.Err != nil && !stream.started.Load() {
				stream.fail(info.Err)
			}
		}
		if !stream.started.Load() {
			if ctx.Err() == nil {
				stream.fail(errTraceStreamClosed)
			}
			stream.finish()
		}
	}()
	cleanup := func() { stream.cancelIO(); stream.cancel(); <-sdkDone; <-stream.readerDone }
	return stream, cleanup
}
