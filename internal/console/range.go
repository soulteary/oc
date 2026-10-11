// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

// byteRange accepts one byte range only. Multiple ranges and malformed syntax
// are rejected explicitly; no multipart response is synthesized.
func byteRange(value string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") || size <= 0 {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	parse := func(v string) (int64, error) {
		if v == "" || strings.IndexFunc(v, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return 0, fmt.Errorf("invalid range")
		}
		return strconv.ParseInt(v, 10, 64)
	}
	if parts[0] == "" {
		suffix, err := parse(parts[1])
		if err != nil || suffix == 0 {
			return 0, 0, false
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, true
	}
	start, err := parse(parts[0])
	if err != nil || start >= size {
		return 0, 0, false
	}
	end := size - 1
	if parts[1] != "" {
		end, err = parse(parts[1])
		if err != nil || end < start {
			return 0, 0, false
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true
}

func etagMatches(list, tag string, weak bool) bool {
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "*" {
			return true
		}
		if weak {
			item = strings.TrimPrefix(item, "W/")
		}
		if item == tag {
			return true
		}
	}
	return false
}
func notModifiedSince(value string, modified time.Time) bool {
	date, err := http.ParseTime(value)
	return err == nil && !modified.IsZero() && !modified.Truncate(time.Second).After(date)
}

func ifRangeDateMatches(value string, modified time.Time) bool {
	date, err := http.ParseTime(value)
	return err == nil && !modified.IsZero() && modified.Truncate(time.Second).Equal(date)
}

// downloadRange must be called only inside the existing authenticated download
// route, with its request lease, joined cancellation and download slot held.
// It intentionally does not register a public route or obtain another identity.
func (s *Server) downloadRange(w http.ResponseWriter, r *http.Request, ctx context.Context, sess *session, ref consoleapi.ObjectRef) bool {
	backend, ok := sess.runtime.backend.(consoleapi.RangeBackend)
	if !ok {
		writeError(w, 501, "range_unsupported", "This connection does not support conditional byte downloads.")
		return false
	}
	info, err := backend.StatReference(ctx, ref)
	if err != nil {
		writeBackendError(w, err)
		return false
	}
	tag := strconv.Quote(info.ETag)
	w.Header().Set("ETag", tag)
	if !info.Modified.IsZero() {
		w.Header().Set("Last-Modified", info.Modified.UTC().Format(http.TimeFormat))
	}
	w.Header().Set("Accept-Ranges", "bytes")
	if value := r.Header.Get("If-Match"); value != "" && !etagMatches(value, tag, false) {
		return rangeHeaders(w, ctx, 412)
	}
	if date, err := http.ParseTime(r.Header.Get("If-Unmodified-Since")); r.Header.Get("If-Match") == "" && err == nil && !info.Modified.IsZero() && info.Modified.Truncate(time.Second).After(date) {
		return rangeHeaders(w, ctx, 412)
	}
	if value := r.Header.Get("If-None-Match"); value != "" {
		if etagMatches(value, tag, true) {
			return rangeHeaders(w, ctx, 304)
		}
	} else if notModifiedSince(r.Header.Get("If-Modified-Since"), info.Modified) {
		return rangeHeaders(w, ctx, 304)
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
		return rangeHeaders(w, ctx, 200)
	}
	value := r.Header.Get("Range")
	if condition := r.Header.Get("If-Range"); value != "" && condition != "" && condition != tag && !ifRangeDateMatches(condition, info.Modified) {
		value = ""
	}
	if value == "" {
		return s.downloadUsing(w, ctx, ref.Key, func(ctx context.Context) (consoleapi.Object, error) {
			return backend.OpenReference(ctx, ref, info.ETag)
		})
	}
	start, end, ok := byteRange(value, info.Size)
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", info.Size))
		return rangeHeaders(w, ctx, 416)
	}
	object, err := backend.OpenRange(ctx, ref, info, start, end)
	if err != nil {
		if object.Body != nil {
			_ = object.Body.Close()
		}
		writeBackendError(w, err)
		return false
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, info.Size))
	return s.downloadUsing(rangeResponse{w}, ctx, ref.Key, func(context.Context) (consoleapi.Object, error) { return object, nil })
}

// rangeHeaders flushes HEAD and conditional responses with the same bounded,
// cancelable downstream writes as object streams. It leaves the deadline set
// until net/http finishes the response, preserving final-chunk protection.
func rangeHeaders(w http.ResponseWriter, ctx context.Context, status int) bool {
	writer := &flushWriter{ctx: ctx, response: w, controller: http.NewResponseController(w)}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(done); writer.cancelWrite() })
	defer func() {
		if !stop() {
			<-done
		}
	}()
	if status != http.StatusNotModified && w.Header().Get("Content-Length") == "" {
		w.Header().Set("Content-Length", "0")
	}
	w.WriteHeader(status)
	_, err := writer.Write(nil)
	if err != nil {
		if writer.controller.SetWriteDeadline(time.Now()) == nil {
			panic(http.ErrAbortHandler)
		}
	}
	return err == nil
}

// Unwrap preserves ResponseController deadlines and flushes.
type rangeResponse struct{ http.ResponseWriter }

func (w rangeResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w rangeResponse) WriteHeader(status int) {
	if status == 200 {
		status = 206
	}
	w.ResponseWriter.WriteHeader(status)
}
