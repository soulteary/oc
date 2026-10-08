// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	minio "github.com/soulteary/otterio-sdk/v7"
)

type pipelineReadFunc func([]byte) (int, error)

func (f pipelineReadFunc) Read(p []byte) (int, error) { return f(p) }

type pipelineRoundTripFunc func(*http.Request) (*http.Response, error)

func (f pipelineRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func waitPipelineSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestMultipartPipelineReadsNextPartDuringPutAndPreservesBothBuffers(t *testing.T) {
	const size = 2*uploadPartSize + 3
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstPutStarted, secondReadStarted, secondReadFinished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	generated := &generatedUploadReader{remaining: size}
	var readsActive, maximumReads, putsActive, maximumPuts, puts atomic.Int32
	reader := pipelineReadFunc(func(p []byte) (int, error) {
		active := readsActive.Add(1)
		defer readsActive.Add(-1)
		maximumReads.CompareAndSwap(0, active)
		if active > 1 {
			t.Error("borrowed body had concurrent readers")
		}
		second := generated.read == uploadPartSize
		if second {
			close(secondReadStarted)
			select {
			case <-firstPutStarted:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		n, err := generated.Read(p)
		if second {
			close(secondReadFinished)
		}
		return n, err
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		q := r.URL.Query()
		switch {
		case q.Has("uploads"):
			fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>pipeline-owned</UploadId></InitiateMultipartUploadResult>`)
		case q.Has("partNumber"):
			active := putsActive.Add(1)
			defer putsActive.Add(-1)
			maximumPuts.CompareAndSwap(0, active)
			if active != 1 {
				t.Error("pipeline dispatched overlapping upstream parts")
			}
			number := puts.Add(1)
			if q.Get("partNumber") != strconv.Itoa(int(number)) {
				t.Error("parts were reordered or retried")
			}
			if number == 1 {
				close(firstPutStarted)
				select {
				case <-secondReadFinished:
				case <-r.Context().Done():
					return
				}
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error("part body failed")
			}
			checkPayloadHashes(t, r, data)
			for offset, value := range data {
				if value != byte((int64(number-1)*uploadPartSize+int64(offset))%251) {
					t.Error("a part buffer was reused before its PUT finished")
					break
				}
			}
			w.Header().Set("ETag", fmt.Sprintf(`"part-%d"`, number))
		case r.Method == http.MethodPost:
			fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>bucket</Bucket><Key>object</Key><ETag>complete</ETag></CompleteMultipartUploadResult>`)
		default:
			t.Error("successful pipeline unexpectedly cleaned or dispatched")
		}
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	result, err := client.Upload(ctx, "bucket", "object", reader, size, consoleapi.UploadOptions{Overwrite: true}, nil)
	if err != nil || result.Size != size || puts.Load() != 3 || maximumReads.Load() != 1 || maximumPuts.Load() != 1 {
		t.Fatalf("bounded pipeline failed: %v", err)
	}
	waitPipelineSignal(t, secondReadStarted, "overlapped body read")
}

func TestMultipartPipelineAcknowledgesWhileNextReadIsBlockedAndCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	secondReadStarted, acknowledged := make(chan struct{}), make(chan struct{})
	generated := &generatedUploadReader{remaining: uploadPartSize + 1}
	reader := pipelineReadFunc(func(p []byte) (int, error) {
		if generated.read == uploadPartSize {
			close(secondReadStarted)
			<-ctx.Done() // The body owner, rather than an internal reader goroutine, interrupts this read.
			return 0, ctx.Err()
		}
		return generated.Read(p)
	})
	var puts, aborts, completions, progressCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		q := r.URL.Query()
		switch {
		case q.Has("uploads"):
			fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>pipeline-owned</UploadId></InitiateMultipartUploadResult>`)
		case q.Has("partNumber"):
			puts.Add(1)
			io.Copy(io.Discard, r.Body)
			w.Header().Set("ETag", `"acknowledged"`)
		case r.Method == http.MethodDelete:
			aborts.Add(1)
			if q.Get("uploadId") != "pipeline-owned" || r.Context().Err() != nil {
				t.Error("cleanup did not use its owned ID and independent context")
			}
			w.WriteHeader(204)
		default:
			completions.Add(1)
		}
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	done := make(chan error, 1)
	go func() {
		_, err := client.Upload(ctx, "bucket", "object", reader, uploadPartSize+1, consoleapi.UploadOptions{Overwrite: true}, func(n int64) {
			if n != uploadPartSize || progressCalls.Add(1) != 1 {
				t.Error("progress was not the first upstream acknowledgement")
			}
			close(acknowledged)
		})
		done <- err
	}()
	waitPipelineSignal(t, secondReadStarted, "blocked second read")
	waitPipelineSignal(t, acknowledged, "ACK during the blocked body read")
	cancel()
	select {
	case err := <-done:
		assertAPIError(t, err, 499, "Canceled")
	case <-time.After(5 * time.Second):
		t.Fatal("canceled pipeline kept its body read or PUT worker")
	}
	if puts.Load() != 1 || aborts.Load() != 1 || completions.Load() != 0 || progressCalls.Load() != 1 {
		t.Fatal("canceled pipeline retried, completed, or emitted late progress")
	}
}

func TestMultipartPipelineReadFailureJoinsCanceledPutBeforeAbort(t *testing.T) {
	putStarted, putCanceled, finishPut := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-finishPut:
		default:
			close(finishPut)
		}
	}()
	generated := &generatedUploadReader{remaining: uploadPartSize + 1}
	reader := pipelineReadFunc(func(p []byte) (int, error) {
		if generated.read == uploadPartSize {
			<-putStarted
			return 0, io.ErrUnexpectedEOF
		}
		return generated.Read(p)
	})
	var puts, aborts, completions, putsActive atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.URL.Query().Has("uploads") {
			fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>pipeline-owned</UploadId></InitiateMultipartUploadResult>`)
		} else if r.Method == http.MethodDelete {
			aborts.Add(1)
			if putsActive.Load() != 0 || r.URL.Query().Get("uploadId") != "pipeline-owned" {
				t.Error("abort raced a live PUT or selected another upload")
			}
			w.WriteHeader(204)
		} else {
			completions.Add(1)
		}
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	opts := client.s3Options
	base := opts.Transport
	opts.Transport = pipelineRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !r.URL.Query().Has("partNumber") {
			return base.RoundTrip(r)
		}
		puts.Add(1)
		putsActive.Add(1)
		defer putsActive.Add(-1)
		close(putStarted)
		<-r.Context().Done()
		close(putCanceled)
		<-finishPut
		return nil, r.Context().Err()
	})
	var err error
	client.s3, err = minio.New(client.s3Endpoint, &opts)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := client.Upload(context.Background(), "bucket", "object", reader, uploadPartSize+1, consoleapi.UploadOptions{Overwrite: true}, nil)
		done <- err
	}()
	waitPipelineSignal(t, putCanceled, "PUT cancellation after a body failure")
	if aborts.Load() != 0 {
		t.Fatal("cleanup started before the PUT worker stopped")
	}
	select {
	case <-done:
		t.Fatal("Upload returned without joining its pending PUT")
	default:
	}
	close(finishPut)
	select {
	case err := <-done:
		assertAPIError(t, err, 400, "upload_size_mismatch")
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline worker was not joined after cancellation")
	}
	if puts.Load() != 1 || aborts.Load() != 1 || completions.Load() != 0 || putsActive.Load() != 0 {
		t.Fatal("failed read retried or leaked a PUT worker")
	}
}

func TestMultipartPipelinePartFailurePreservesErrorAndBorrowedReaderOwnership(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	secondReadStarted, releaseRead := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-releaseRead:
		default:
			close(releaseRead)
		}
	}()
	generated := &generatedUploadReader{remaining: uploadPartSize + 1}
	reader := pipelineReadFunc(func(p []byte) (int, error) {
		if generated.read == uploadPartSize {
			close(secondReadStarted)
			<-releaseRead // Simulates the HTTP owner's existing idle/read cancellation boundary.
			return 0, io.EOF
		}
		return generated.Read(p)
	})
	var puts, aborts, completions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		q := r.URL.Query()
		switch {
		case q.Has("uploads"):
			fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>pipeline-owned</UploadId></InitiateMultipartUploadResult>`)
		case q.Has("partNumber"):
			puts.Add(1)
			io.Copy(io.Discard, r.Body)
			<-secondReadStarted
			w.WriteHeader(403)
			fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>secret-marker upstream denial</Message></Error>`)
		case r.Method == http.MethodDelete:
			aborts.Add(1)
			w.WriteHeader(204)
		default:
			completions.Add(1)
		}
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	opts := client.s3Options
	base := opts.Transport
	putContext := make(chan context.Context, 1)
	opts.Transport = pipelineRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Has("partNumber") {
			putContext <- r.Context()
		}
		return base.RoundTrip(r)
	})
	var err error
	client.s3, err = minio.New(client.s3Endpoint, &opts)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := client.Upload(ctx, "bucket", "object", reader, uploadPartSize+1, consoleapi.UploadOptions{Overwrite: true}, nil)
		done <- err
	}()
	waitPipelineSignal(t, secondReadStarted, "borrowed-body read")
	select {
	case partCtx := <-putContext:
		waitPipelineSignal(t, partCtx.Done(), "internal upload cancellation after part denial")
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline never dispatched its first PUT")
	}
	if ctx.Err() != nil || aborts.Load() != 0 {
		t.Fatal("part failure canceled the body owner's context or skipped its blocked read")
	}
	select {
	case <-done:
		t.Fatal("Upload abandoned a still-blocked borrowed body read")
	default:
	}
	close(releaseRead)
	select {
	case err := <-done:
		assertAPIError(t, err, 403, "AccessDenied")
	case <-time.After(5 * time.Second):
		t.Fatal("part failure did not finish after the body owner released its read")
	}
	if puts.Load() != 1 || aborts.Load() != 1 || completions.Load() != 0 {
		t.Fatal("denied pipeline retried or completed")
	}
}
