// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"net/http"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	minio "github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
)

const (
	conditionalCopyHeader   = "X-Otterio-Conditional-Copy"
	conditionalDeleteHeader = "X-Otterio-Conditional-Delete"
)

func renameUnsupported() *consoleapi.Error {
	return &consoleapi.Error{Status: http.StatusNotImplemented, Code: "rename_unsupported", Message: "This storage server cannot safely rename objects. Conditional copy and conditional delete support with Signature V4 are required."}
}

func renamePartial(changed bool) *consoleapi.Error {
	if changed {
		return &consoleapi.Error{Status: http.StatusConflict, Code: "rename_partial", Message: "The new object was copied, but the source changed or disappeared before deletion. Refresh both names before retrying."}
	}
	return &consoleapi.Error{Status: http.StatusBadGateway, Code: "rename_partial", Message: "The new object was copied, but source deletion was not confirmed. Check both names before retrying."}
}

func (c *Client) statRenameObject(ctx context.Context, bucket, key string) (minio.ObjectInfo, *conditionalCapability, error) {
	headCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	observation := &conditionalCapability{}
	headCtx = context.WithValue(headCtx, capabilityContextKey{}, observation)
	info, err := c.s3.StatObject(headCtx, bucket, key, minio.StatObjectOptions{})
	return info, observation, err
}

// RenameObject copies to an absent destination, then conditionally deletes the
// source with the same ETag. Each condition is checked under a server commit
// lock. The two requests are not an atomic rename: failed or unconfirmed source
// deletion leaves the destination in place and reports a partial outcome.
func (c *Client) RenameObject(ctx context.Context, bucket, key, newKey, etag string) error {
	etag = canonicalETag(etag)
	if err := c.ready(ctx); err != nil {
		return err
	}
	if !validMutationTarget(bucket, key) || !validMutationTarget(bucket, newKey) || key == newKey || !validETag(etag) || etag == "*" {
		return invalidRequest()
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil {
		return normalizeError(err)
	}
	if credential.SignerType != credentials.SignatureV4 {
		return renameUnsupported()
	}
	_, targetCapability, err := c.statRenameObject(ctx, bucket, newKey)
	if err == nil {
		return objectExists()
	}
	response := minio.ToErrorResponse(err)
	if response.StatusCode != http.StatusNotFound || (response.Code != "NoSuchKey" && response.Code != "NoSuchObject" && response.Code != "NotFound") {
		return normalizeError(err)
	}
	// Create-only PUT support does not imply conditional CopyObject support.
	if !targetCapability.copySupported.Load() {
		return renameUnsupported()
	}
	source, sourceCapability, err := c.statRenameObject(ctx, bucket, key)
	if err != nil {
		return normalizeError(err)
	}
	if !sourceCapability.deleteSupported.Load() {
		return renameUnsupported()
	}
	if source.ETag != etag {
		return referenceChanged()
	}
	if source.Size > maxUploadSize {
		return &consoleapi.Error{Status: http.StatusRequestEntityTooLarge, Code: "rename_too_large", Message: "Rename supports objects up to 5 GiB."}
	}
	opts := minio.PutObjectOptions{}
	opts.SetMatchETagExcept("*")
	core := minio.Core{Client: c.s3}
	copied, err := core.CopyObject(ctx, bucket, key, bucket, newKey, map[string]string{"If-None-Match": "*", "X-Amz-Copy-Source-If-Match": "\"" + etag + "\""}, minio.CopySrcOptions{MatchETag: etag}, opts)
	if err != nil {
		if response := minio.ToErrorResponse(err); response.Code == "PreconditionFailed" || response.Code == "ConditionalRequestConflict" {
			return &consoleapi.Error{Status: http.StatusConflict, Code: "rename_conflict", Message: "The source changed or an object appeared at the new name. Refresh both names before retrying."}
		}
		return mutationFailure(err)
	}
	if !validETag(copied.ETag) || copied.ETag == "*" {
		return outcomeUnknown()
	}
	return c.deleteRenameSource(ctx, bucket, key, etag)
}

func (c *Client) deleteRenameSource(ctx context.Context, bucket, key, etag string) error {
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	headers := make(http.Header)
	headers.Set("If-Match", "\""+etag+"\"")
	// The pinned SDK's RemoveObjectOptions has no condition field. Its public
	// presigner preserves the selected identity, region, endpoint and opaque key
	// while binding If-Match to a single DELETE. This URL stays inside OC.
	u, err := c.s3.PresignHeader(requestCtx, http.MethodDelete, bucket, key, time.Minute, nil, headers)
	if err != nil {
		return renamePartial(false)
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return renamePartial(false)
	}
	req.Header = headers
	// RoundTrip performs this mutation once and does not follow redirects.
	resp, err := c.s3Transport.RoundTrip(req)
	if err != nil {
		return renamePartial(false)
	}
	data, readErr := readBounded(resp.Body, maxManagementReply)
	if readErr != nil {
		return renamePartial(false)
	}
	if resp.StatusCode == http.StatusPreconditionFailed || resp.StatusCode == http.StatusNotFound {
		return renamePartial(true)
	}
	if resp.StatusCode != http.StatusNoContent || len(data) != 0 {
		return renamePartial(false)
	}
	return nil
}
