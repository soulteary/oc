// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/soulteary/mc/internal/consoleapi"
	minio "github.com/soulteary/otterio-sdk/v7"
)

// OpenRange opens a closed byte interval against metadata obtained using this
// same identity. Both the version and ETag remain pinned across HEAD and GET.
// A server ignoring Range is rejected before any bytes reach the browser.
func (c *Client) OpenRange(ctx context.Context, ref consoleapi.ObjectRef, expected consoleapi.ObjectInfo, start, end int64) (consoleapi.Object, error) {
	if err := c.ready(ctx); err != nil {
		return consoleapi.Object{}, err
	}
	if !validReference(ref) || !validETag(expected.ETag) || start < 0 || end < start || end >= expected.Size {
		return consoleapi.Object{}, invalidRequest()
	}
	opts := minio.GetObjectOptions{VersionID: ref.VersionID}
	if err := opts.SetMatchETag(expected.ETag); err != nil {
		return consoleapi.Object{}, invalidRequest()
	}
	if err := opts.SetRange(start, end); err != nil {
		return consoleapi.Object{}, invalidRequest()
	}
	body, info, headers, err := (minio.Core{Client: c.s3}).GetObject(ctx, ref.Bucket, ref.Key, opts)
	reject := func(err error) (consoleapi.Object, error) {
		if body != nil {
			_ = body.Close()
		}
		return consoleapi.Object{}, err
	}
	if err != nil {
		return reject(featureReadError(err))
	}
	if ref.VersionID != "" && headers.Get("X-Otterio-Version-Authorization") != "v1" {
		return reject(versionAuthorizationUnsupported())
	}
	if body == nil || info.ETag != expected.ETag || !versionMatches(ref.VersionID, info.VersionID) || !sameRangeVersion(expected.VersionID, info.VersionID) || !expected.Modified.Equal(info.LastModified) || headers.Get("X-Amz-Delete-Marker") == "true" {
		return reject(referenceChanged())
	}
	length := end - start + 1
	if headers.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end, expected.Size) || info.Size != length {
		return reject(&consoleapi.Error{Status: http.StatusBadGateway, Code: "range_unsupported", Message: "The storage server did not honor the requested byte range."})
	}
	return consoleapi.Object{Body: body, Size: length, ContentType: info.ContentType, Modified: info.LastModified, ETag: info.ETag}, nil
}

// Unlike a requested empty VersionID (which selects the current object), an
// empty version observed by HEAD identifies the unversioned/null object. It
// must never act as a wildcard when validating the subsequent GET response.
func sameRangeVersion(expected, actual string) bool {
	if expected == "null" {
		expected = ""
	}
	if actual == "null" {
		actual = ""
	}
	return expected == actual
}

// OpenRangeFull uses the original selector: a current-object request must not
// become a historical-version request merely because HEAD observed a version.
// Both full conditional GET and If-Range fallback therefore retain GetObject
// authorization while rejecting races in the returned metadata.
func (c *Client) OpenRangeFull(ctx context.Context, ref consoleapi.ObjectRef, expected consoleapi.ObjectInfo) (consoleapi.Object, error) {
	if err := c.ready(ctx); err != nil {
		return consoleapi.Object{}, err
	}
	if !validReference(ref) || !validETag(expected.ETag) || expected.Size < 0 {
		return consoleapi.Object{}, invalidRequest()
	}
	opts := minio.GetObjectOptions{VersionID: ref.VersionID}
	if err := opts.SetMatchETag(expected.ETag); err != nil {
		return consoleapi.Object{}, invalidRequest()
	}
	body, info, headers, err := (minio.Core{Client: c.s3}).GetObject(ctx, ref.Bucket, ref.Key, opts)
	reject := func(err error) (consoleapi.Object, error) {
		if body != nil {
			_ = body.Close()
		}
		return consoleapi.Object{}, err
	}
	if err != nil {
		return reject(featureReadError(err))
	}
	if ref.VersionID != "" && headers.Get("X-Otterio-Version-Authorization") != "v1" {
		return reject(versionAuthorizationUnsupported())
	}
	if body == nil || info.ETag != expected.ETag || info.Size != expected.Size || !versionMatches(ref.VersionID, info.VersionID) || !sameRangeVersion(expected.VersionID, info.VersionID) || !expected.Modified.Equal(info.LastModified) || headers.Get("X-Amz-Delete-Marker") == "true" || headers.Get("Content-Range") != "" {
		return reject(referenceChanged())
	}
	return consoleapi.Object{Body: body, Size: info.Size, ETag: info.ETag, ContentType: info.ContentType, Modified: info.LastModified}, nil
}
