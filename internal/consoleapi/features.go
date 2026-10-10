// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package consoleapi

import (
	"context"
	"time"
)

// ObjectRef is an exact S3 object reference. Empty VersionID selects the current
// object; "null" explicitly selects the null version and is never discarded.
type ObjectRef struct {
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	VersionID string `json:"versionId,omitempty"`
}

type ObjectInfo struct {
	Size        int64     `json:"size"`
	ETag        string    `json:"etag"`
	VersionID   string    `json:"versionId,omitempty"`
	ContentType string    `json:"contentType,omitempty"`
	Modified    time.Time `json:"modified"`
}

// ReferenceBackend supports version-aware downloads and fixed archive plans.
// OpenReference must honor both the version and a nonempty expected ETag, and
// fail rather than returning a newer/current object when either no longer exists.
type ReferenceBackend interface {
	StatReference(context.Context, ObjectRef) (ObjectInfo, error)
	OpenReference(ctx context.Context, ref ObjectRef, expectedETag string) (Object, error)
}

// BucketBackend deletes only an empty bucket; it never empties a bucket or
// removes historical versions, deletion markers or multipart uploads.
type BucketBackend interface {
	CreateBucket(ctx context.Context, bucket string) error
	DeleteBucket(ctx context.Context, bucket string) error
}

type VersionEntry struct {
	Key          string    `json:"key"`
	VersionID    string    `json:"versionId"`
	DeleteMarker bool      `json:"deleteMarker"`
	Latest       bool      `json:"latest"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag,omitempty"`
	Modified     time.Time `json:"modified"`
}

type VersionPage struct {
	Entries    []VersionEntry `json:"entries"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

// VersionBackend returns one bounded page for one exact key, in upstream order.
type VersionBackend interface {
	ListVersions(ctx context.Context, bucket, key, cursor string, limit int) (VersionPage, error)
}

type ShareRequest struct {
	ObjectRef
	ExpiresSeconds int64  `json:"expiresSeconds"`
	DownloadName   string `json:"downloadName,omitempty"`
}

type Share struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// ShareBackend only signs GET links. A Share is a bearer capability and must
// never be stored in task history, server logs, or error messages.
type ShareBackend interface {
	Presign(context.Context, ShareRequest) (Share, error)
}

// VersionCapabilityBackend checks the server protocol for one authorized bucket.
// A false result means version features must not be offered for this scope.
type VersionCapabilityBackend interface {
	VersionSupported(context.Context, string) (bool, error)
}
