// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	minio "github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/s3utils"
)

const (
	uploadPartSize       = 16 * 1024 * 1024
	maxUploadSize        = 5 * 1024 * 1024 * 1024
	uploadCleanupTimeout = 5 * time.Second
)

var _ consoleapi.MutationBackend = (*Client)(nil)

type capabilityContextKey struct{}
type conditionalCapability struct{ supported atomic.Bool }

// Capability belongs to one HEAD context, never to a process-wide last response.
// Location discovery GETs and concurrent requests cannot grant another upload
// permission to use conditional writes.
type capabilityTransport struct{ base http.RoundTripper }

func (t capabilityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if req.Method == http.MethodHead && resp != nil {
		if observation, ok := req.Context().Value(capabilityContextKey{}).(*conditionalCapability); ok {
			observation.supported.Store(resp.Header.Get("X-Otterio-Conditional-Writes") == "v1")
		}
	}
	return resp, err
}

func validMutationTarget(bucket, key string) bool {
	return s3utils.CheckValidBucketName(bucket) == nil && s3utils.CheckValidObjectName(key) == nil && !strings.ContainsRune(key, 0)
}

func objectExists() *consoleapi.Error {
	return &consoleapi.Error{Status: http.StatusConflict, Code: "object_exists", Message: "An object already exists at this key. Confirm overwrite before uploading."}
}

func outcomeUnknown() *consoleapi.Error {
	return &consoleapi.Error{Status: http.StatusBadGateway, Code: "outcome_unknown", Message: "The storage server did not confirm the result. Check the object before retrying."}
}

func mutationFailure(err error) *consoleapi.Error {
	// A parsed S3 failure is explicit. A lost response, malformed acknowledgement
	// or cancellation after dispatch cannot establish whether a write committed.
	if response := minio.ToErrorResponse(err); response.Code != "" && (response.StatusCode >= 300 || !strings.Contains(response.Code, " ")) {
		return normalizeError(err)
	}
	return outcomeUnknown()
}

func (c *Client) checkCreate(ctx context.Context, bucket, key string) error {
	headCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	observation := &conditionalCapability{}
	headCtx = context.WithValue(headCtx, capabilityContextKey{}, observation)
	_, err := c.s3.StatObject(headCtx, bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return objectExists()
	}
	response := minio.ToErrorResponse(err)
	if response.StatusCode != http.StatusNotFound || (response.Code != "NoSuchKey" && response.Code != "NoSuchObject" && response.Code != "NotFound") {
		return normalizeError(err)
	}
	if !observation.supported.Load() {
		return &consoleapi.Error{Status: http.StatusNotImplemented, Code: "conditional_writes_unsupported", Message: "This storage server cannot guarantee a new key without overwriting. Explicitly confirm overwrite to upload."}
	}
	return nil
}

type uploadReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r uploadReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func checkUploadEnd(reader io.Reader) error {
	var extra [1]byte
	n, err := io.ReadFull(reader, extra[:])
	if n != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func uploadHashes(data []byte) (string, string) {
	// Both hashes only read this part. Join the SHA worker before returning so
	// Upload cannot reuse a part buffer while a hash still reads it.
	var shaSum [sha256.Size]byte
	shaDone := make(chan struct{})
	go func() {
		shaSum = sha256.Sum256(data)
		close(shaDone)
	}()
	md5Sum := md5.Sum(data)
	<-shaDone
	return base64.StdEncoding.EncodeToString(md5Sum[:]), hex.EncodeToString(shaSum[:])
}

type pendingUploadPart struct {
	done chan struct{}
	part minio.CompletePart
	err  error
}

// Upload uses two bounded 16MiB buffers for multipart uploads. Only its calling
// goroutine reads the borrowed body; the next read/hash overlaps one part PUT.
// It never seeks, lists incomplete sessions, or resumes another request's upload.
func (c *Client) Upload(ctx context.Context, bucket, key string, body io.Reader, size int64, opts consoleapi.UploadOptions, progress func(int64)) (result consoleapi.UploadResult, err error) {
	if err := c.ready(ctx); err != nil {
		return result, err
	}
	if !validMutationTarget(bucket, key) || body == nil || size < 0 {
		return result, invalidRequest()
	}
	if size > maxUploadSize {
		return result, &consoleapi.Error{Status: http.StatusRequestEntityTooLarge, Code: "upload_too_large", Message: "The upload exceeds the maximum supported size."}
	}
	if !opts.Overwrite {
		if err := c.checkCreate(ctx, bucket, key); err != nil {
			return result, err
		}
	}
	putOpts := minio.PutObjectOptions{ContentType: "application/octet-stream", DisableContentSha256: true}
	if !opts.Overwrite {
		putOpts.SetMatchETagExcept("*")
	}
	reader := uploadReader{ctx: ctx, reader: body}
	core := minio.Core{Client: c.s3}
	bufferSize := int64(uploadPartSize)
	if size < bufferSize {
		bufferSize = size
	}
	buffer := make([]byte, int(bufferSize))
	if size <= uploadPartSize {
		if _, readErr := io.ReadFull(reader, buffer); readErr != nil {
			return result, uploadReadError(ctx)
		}
		if checkUploadEnd(reader) != nil {
			return result, uploadReadError(ctx)
		}
		if err := ctx.Err(); err != nil {
			return result, normalizeError(err)
		}
		md5Sum, shaSum := uploadHashes(buffer)
		info, putErr := core.PutObject(ctx, bucket, key, bytes.NewReader(buffer), size, md5Sum, shaSum, putOpts)
		if putErr != nil {
			return result, mutationFailure(putErr)
		}
		if info.ETag == "" {
			return result, outcomeUnknown()
		}
		if progress != nil {
			progress(size)
		}
		return consoleapi.UploadResult{ETag: info.ETag, Size: size}, nil
	}
	initCtx, cancelInit := context.WithTimeout(ctx, c.metadataTimeout)
	uploadID, initErr := core.NewMultipartUpload(initCtx, bucket, key, putOpts)
	cancelInit()
	if initErr != nil {
		return result, normalizeError(initErr)
	}
	if uploadID == "" {
		return result, outcomeUnknown()
	}
	uploadCtx, cancelUpload := context.WithCancelCause(ctx)
	reader.ctx = uploadCtx
	buffers := [2][]byte{buffer, make([]byte, uploadPartSize)}
	parts := make([]minio.CompletePart, 0, int((size+uploadPartSize-1)/uploadPartSize))
	var pending *pendingUploadPart
	joinPending := func() error {
		if pending == nil {
			return nil
		}
		<-pending.done
		finished := pending
		pending = nil
		if finished.err == nil {
			parts = append(parts, finished.part)
		}
		return finished.err
	}
	defer func() {
		// Stop and join the only PUT worker before aborting. Its buffer and the
		// owned upload ID cannot be reused or cleaned while a PUT still uses them.
		cause := context.Cause(uploadCtx)
		cancelUpload(context.Canceled)
		if partErr := joinPending(); partErr != nil && cause != nil && ctx.Err() == nil {
			// A part failure can cancel the next body read. Preserve that upstream
			// failure instead of reporting the resulting canceled read as bad input.
			err = partErr
		}
		if err == nil {
			return
		}
		// Cleanup has its own deadline, identity and endpoint remain immutable.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), uploadCleanupTimeout)
		defer cancel()
		cleanupErr := core.AbortMultipartUpload(cleanupCtx, bucket, key, uploadID)
		if cleanupErr != nil && minio.ToErrorResponse(cleanupErr).Code != "NoSuchUpload" {
			err = &consoleapi.Error{Status: http.StatusBadGateway, Code: "cleanup_failed", Message: "The upload did not finish and temporary parts could not be confirmed removed. Check the final object and unfinished upload before retrying."}
		}
	}()
	var readBytes int64
	for number := 1; readBytes < size; number++ {
		partSize := int64(uploadPartSize)
		if remaining := size - readBytes; remaining < partSize {
			partSize = remaining
		}
		part := buffers[(number-1)%len(buffers)][:int(partSize)]
		if _, readErr := io.ReadFull(reader, part); readErr != nil {
			return result, uploadReadError(ctx)
		}
		if readBytes+partSize == size && checkUploadEnd(reader) != nil {
			return result, uploadReadError(ctx)
		}
		if err := ctx.Err(); err != nil {
			return result, normalizeError(err)
		}
		md5Sum, shaSum := uploadHashes(part)
		if partErr := joinPending(); partErr != nil {
			return result, partErr
		}
		if err := uploadCtx.Err(); err != nil {
			return result, normalizeError(err)
		}
		readBytes += partSize
		pending = &pendingUploadPart{done: make(chan struct{})}
		go func(worker *pendingUploadPart, data []byte, number int, partSize, cumulative int64, md5Sum, shaSum string) {
			info, partErr := core.PutObjectPart(uploadCtx, bucket, key, uploadID, number, bytes.NewReader(data), partSize, minio.PutObjectPartOptions{Md5Base64: md5Sum, Sha256Hex: shaSum, DisableContentSha256: true})
			if partErr != nil {
				worker.err = normalizeError(partErr)
			} else if info.ETag == "" {
				worker.err = normalizeError(errors.New("part acknowledgement missing"))
			} else {
				worker.part = minio.CompletePart{PartNumber: number, ETag: info.ETag}
				// Publish each ACK even if the next borrowed-body read is stalled.
				if progress != nil {
					progress(cumulative)
				}
			}
			if worker.err != nil {
				cancelUpload(worker.err)
			}
			close(worker.done)
		}(pending, part, number, partSize, readBytes, md5Sum, shaSum)
	}
	if partErr := joinPending(); partErr != nil {
		return result, partErr
	}
	if err := ctx.Err(); err != nil {
		return result, normalizeError(err)
	}
	completeCtx, cancelComplete := context.WithTimeout(uploadCtx, c.metadataTimeout)
	info, completeErr := core.CompleteMultipartUpload(completeCtx, bucket, key, uploadID, parts, putOpts)
	cancelComplete()
	if completeErr != nil {
		return result, mutationFailure(completeErr)
	}
	if info.Bucket != bucket || info.Key != key || info.ETag == "" {
		return result, outcomeUnknown()
	}
	return consoleapi.UploadResult{ETag: info.ETag, Size: size}, nil
}

func uploadReadError(ctx context.Context) *consoleapi.Error {
	if err := ctx.Err(); err != nil {
		return normalizeError(err)
	}
	return &consoleapi.Error{Status: http.StatusBadRequest, Code: "upload_size_mismatch", Message: "The upload body does not match its declared size or could not be read."}
}

func (c *Client) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := c.ready(ctx); err != nil {
		return err
	}
	if !validMutationTarget(bucket, key) {
		return invalidRequest()
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	// No version ID, force delete, governance bypass, or batch retry is allowed.
	if err := c.s3.RemoveObject(requestCtx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return mutationFailure(err)
	}
	return nil
}

func (c *Client) ScanObjects(ctx context.Context, bucket, prefix, cursor string, limit int) (consoleapi.Page, error) {
	return c.listObjects(ctx, bucket, prefix, cursor, limit, "")
}
