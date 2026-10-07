// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

type contextResponseWriter struct {
	http.ResponseWriter
	ctx context.Context
}

func (w *contextResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// writePayload flushes known-length responses while the deadline is still
// active. No chunk terminator or buffered JSON remains for finishRequest to
// write after this function has restored the connection's deadline.
func writePayload(w http.ResponseWriter, status int, payload []byte) {
	ctx := context.Background()
	if contextual, ok := w.(*contextResponseWriter); ok {
		ctx = contextual.ctx
	}
	controller := http.NewResponseController(w)
	// Early failures may have an unread raw upload body. Full duplex prevents
	// Flush from waiting to drain that body before sending the error response;
	// ServeHTTP closes it under an immediate read deadline before returning.
	_ = controller.EnableFullDuplex()
	var mu sync.Mutex
	setDeadline := func(cancel bool) {
		mu.Lock()
		defer mu.Unlock()
		deadline := time.Now()
		if !cancel && ctx.Err() == nil {
			deadline = deadline.Add(30 * time.Second)
		}
		_ = controller.SetWriteDeadline(deadline)
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(done); setDeadline(true) })
	defer func() {
		if !stop() {
			<-done
		}
		_ = controller.SetWriteDeadline(time.Time{})
	}()
	setDeadline(false)
	if status != 0 {
		w.WriteHeader(status)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
	if err := controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		panic(http.ErrAbortHandler)
	}
}
