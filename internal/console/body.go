// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type trackedRequestBody struct {
	io.ReadCloser
	exhausted atomic.Bool
	closed    atomic.Bool
	once      sync.Once
}

func (b *trackedRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.exhausted.Store(true)
	}
	return n, err
}
func (b *trackedRequestBody) Close() error {
	var err error
	b.once.Do(func() { err = b.ReadCloser.Close(); b.closed.Store(true) })
	return err
}
func bodyNeedsInterrupt(body io.ReadCloser) bool {
	if body == nil || body == http.NoBody {
		return false
	}
	if tracked, ok := body.(*trackedRequestBody); ok {
		return !tracked.exhausted.Load() && !tracked.closed.Load()
	}
	return true
}

// requestBody interrupts a blocked socket read before closing the body. Closing
// a net/http request body alone can wait for the in-progress Read's mutex.
type requestBody struct {
	ctx        context.Context
	body       io.ReadCloser
	controller *http.ResponseController
	idle       time.Duration
	mu         sync.Mutex
	once       sync.Once
}

func (b *requestBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	if err := b.ctx.Err(); err != nil {
		b.mu.Unlock()
		return 0, err
	}
	_ = b.controller.SetReadDeadline(time.Now().Add(b.idle))
	b.mu.Unlock()
	return b.body.Read(p)
}

func (b *requestBody) Close() error {
	b.once.Do(func() {
		b.mu.Lock()
		if bodyNeedsInterrupt(b.body) {
			_ = b.controller.SetReadDeadline(time.Now())
		}
		b.mu.Unlock()
		_ = b.body.Close()
	})
	return nil
}

func boundedBody(w http.ResponseWriter, r *http.Request, ctx context.Context, idle time.Duration) (*requestBody, func()) {
	b := &requestBody{ctx: ctx, body: r.Body, controller: http.NewResponseController(w), idle: idle}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(done); _ = b.Close() })
	return b, func() {
		if !stop() {
			<-done
		}
		_ = b.Close()
		_ = b.controller.SetReadDeadline(time.Time{})
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any, max int64) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, 415, "invalid_input", "This request requires JSON.")
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	body, closeBody := boundedBody(w, r, ctx, 10*time.Second)
	defer closeBody()
	decoder := json.NewDecoder(http.MaxBytesReader(w, body, max))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, 400, "invalid_input", "The request body is invalid or timed out.")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, 400, "invalid_input", "The request body is invalid.")
		return false
	}
	return true
}

// readWithIdleTimeout bounds an individual upstream body Read. Time spent
// waiting for the browser to receive a chunk is bounded separately by Write.
type readWithIdleTimeout struct {
	body      io.Reader
	idle      time.Duration
	interrupt func()
}

func (r readWithIdleTimeout) Read(p []byte) (int, error) {
	done := make(chan struct{})
	timer := time.AfterFunc(r.idle, func() { defer close(done); r.interrupt() })
	n, err := r.body.Read(p)
	if !timer.Stop() {
		<-done
	}
	return n, err
}
