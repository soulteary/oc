// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

// Package consoleapi defines the storage boundary used by the local console,
// without filesystem access or CLI configuration. Existing credentials never
// cross it; generated IAM credentials are returned only by their creation call.
package consoleapi

import (
	"context"
	"io"
	"time"
)

type Bucket struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

type Entry struct {
	Key      string    `json:"key"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	ETag     string    `json:"etag,omitempty"`
	IsPrefix bool      `json:"isPrefix"`
}

type Page struct {
	Entries    []Entry `json:"entries"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

// Object owns an upstream stream; the caller must always close Body, including
// when the browser disconnects before consuming the entire object.
type Object struct {
	Body        io.ReadCloser
	Size        int64
	ContentType string
	Modified    time.Time
	ETag        string
}

type AccountBucket struct {
	Name  string `json:"name"`
	Size  uint64 `json:"size"`
	Read  bool   `json:"read"`
	Write bool   `json:"write"`
}

type Account struct {
	Buckets []AccountBucket `json:"buckets"`
}

// Backend performs each request using the startup-selected user's identity.
// These methods only read storage and cannot select another local alias.
type Backend interface {
	ListBuckets(context.Context) ([]Bucket, error)
	ListObjects(ctx context.Context, bucket, prefix, cursor string, limit int) (Page, error)
	OpenObject(ctx context.Context, bucket, key string) (Object, error)
	AccountInfo(context.Context) (Account, error)
}

// MutationBackend is opt-in. Upload borrows body; its caller owns closing it on
// cancellation. Progress counts bytes acknowledged upstream, not browser sends.
// DeleteObject deletes the current key without a version ID or retention bypass.
type MutationBackend interface {
	Upload(ctx context.Context, bucket, key string, body io.Reader, size int64, opts UploadOptions, progress func(int64)) (UploadResult, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	ScanObjects(ctx context.Context, bucket, prefix, cursor string, limit int) (Page, error)
}

type UploadOptions struct {
	Overwrite bool
}

type UploadResult struct {
	ETag string `json:"etag,omitempty"`
	Size int64  `json:"size"`
}

// Job is a credential-free snapshot. ConfirmToken appears only in a newly
// created deletion plan; status reads never disclose or regenerate it.
type Job struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`   // upload or delete
	Status       string    `json:"status"` // waiting, planning, ready, running, succeeded, failed, partial, canceled
	Bucket       string    `json:"bucket"`
	Key          string    `json:"key,omitempty"`
	Prefix       string    `json:"prefix,omitempty"`
	Size         int64     `json:"size"`
	Transferred  int64     `json:"transferred"`
	Overwrite    bool      `json:"overwrite,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	Created      time.Time `json:"created"`
	Expires      time.Time `json:"expires"`
	Count        int       `json:"count"`
	Completed    int       `json:"completed"`
	Items        []JobItem `json:"items,omitempty"`
	Error        *Error    `json:"error,omitempty"`
	ConfirmToken string    `json:"confirmToken,omitempty"`
}

type JobItem struct {
	Key    string `json:"key"`
	Status string `json:"status"` // pending, succeeded, failed, unknown
	Error  *Error `json:"error,omitempty"`
}

// Error contains only a safe, user-facing message. Backend implementations must
// not put raw upstream errors, target URLs, credentials, or signed URLs here.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }
