// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/soulteary/mc/internal/consoleapi"
	minio "github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/s3utils"
)

var (
	_ consoleapi.BucketBackend    = (*Client)(nil)
	_ consoleapi.VersionBackend   = (*Client)(nil)
	_ consoleapi.ReferenceBackend = (*Client)(nil)
	_ consoleapi.ShareBackend     = (*Client)(nil)
)

func validVersionID(version string) bool {
	return len(version) <= 1024 && utf8.ValidString(version) && !strings.ContainsAny(version, "\x00\r\n")
}

func validReference(ref consoleapi.ObjectRef) bool {
	return validMutationTarget(ref.Bucket, ref.Key) && validVersionID(ref.VersionID)
}

func referenceChanged() *consoleapi.Error {
	return &consoleapi.Error{Status: http.StatusConflict, Code: "object_changed", Message: "The selected object or version has changed. Refresh the list and try again."}
}

func versionAuthorizationUnsupported() *consoleapi.Error {
	return &consoleapi.Error{Status: http.StatusNotImplemented, Code: "versions_unsupported", Message: "This server does not advertise protected version authorization. Upgrade the server before reading or sharing historical versions."}
}

// Core listing returns the wire ETag, while StatObject returns its bare value.
// Strip only one complete quoting pair; malformed or unsafe values still fail
// validETag and never reach a request header.
func canonicalETag(etag string) string {
	if len(etag) >= 2 && etag[0] == '"' && etag[len(etag)-1] == '"' {
		return etag[1 : len(etag)-1]
	}
	return etag
}

func validETag(etag string) bool {
	return etag != "" && len(etag) <= 512 && !strings.ContainsAny(etag, "\"\\\x00\r\n")
}

func versionMatches(requested, actual string) bool {
	// S3 servers may omit the header for the unversioned null object. Every
	// non-null reference requires an explicit matching acknowledgement.
	return requested == "" || requested == actual || (requested == "null" && actual == "")
}

func (c *Client) StatReference(ctx context.Context, ref consoleapi.ObjectRef) (consoleapi.ObjectInfo, error) {
	if err := c.ready(ctx); err != nil {
		return consoleapi.ObjectInfo{}, err
	}
	if !validReference(ref) {
		return consoleapi.ObjectInfo{}, invalidRequest()
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	info, err := c.s3.StatObject(requestCtx, ref.Bucket, ref.Key, minio.StatObjectOptions{VersionID: ref.VersionID})
	if err != nil {
		return consoleapi.ObjectInfo{}, featureReadError(err)
	}
	if ref.VersionID != "" && info.Headers.Get("X-Otterio-Version-Authorization") != "v1" {
		return consoleapi.ObjectInfo{}, versionAuthorizationUnsupported()
	}
	if !versionMatches(ref.VersionID, info.VersionID) || !validETag(info.ETag) || info.Size < 0 {
		return consoleapi.ObjectInfo{}, referenceChanged()
	}
	return consoleapi.ObjectInfo{Size: info.Size, ETag: info.ETag, VersionID: info.VersionID, ContentType: info.ContentType, Modified: info.LastModified}, nil
}

func (c *Client) OpenReference(ctx context.Context, ref consoleapi.ObjectRef, expectedETag string) (consoleapi.Object, error) {
	if err := c.ready(ctx); err != nil {
		return consoleapi.Object{}, err
	}
	if !validReference(ref) || (expectedETag != "" && !validETag(expectedETag)) {
		return consoleapi.Object{}, invalidRequest()
	}
	if expectedETag == "" {
		info, err := c.StatReference(ctx, ref)
		if err != nil {
			return consoleapi.Object{}, err
		}
		expectedETag = info.ETag
	}
	opts := minio.GetObjectOptions{VersionID: ref.VersionID}
	if err := opts.SetMatchETag(expectedETag); err != nil {
		return consoleapi.Object{}, invalidRequest()
	}
	body, info, headers, err := (minio.Core{Client: c.s3}).GetObject(ctx, ref.Bucket, ref.Key, opts)
	if err != nil {
		if body != nil {
			_ = body.Close()
		}
		return consoleapi.Object{}, featureReadError(err)
	}
	if ref.VersionID != "" && headers.Get("X-Otterio-Version-Authorization") != "v1" {
		if body != nil {
			_ = body.Close()
		}
		return consoleapi.Object{}, versionAuthorizationUnsupported()
	}
	if body == nil || info.ETag != expectedETag || !versionMatches(ref.VersionID, info.VersionID) || headers.Get("X-Amz-Delete-Marker") == "true" {
		if body != nil {
			_ = body.Close()
		}
		return consoleapi.Object{}, referenceChanged()
	}
	return consoleapi.Object{Body: body, Size: info.Size, ContentType: info.ContentType, Modified: info.LastModified, ETag: info.ETag}, nil
}

func featureReadError(err error) *consoleapi.Error {
	switch minio.ToErrorResponse(err).Code {
	case "PreconditionFailed", "ConditionalRequestConflict":
		return referenceChanged()
	case "NoSuchVersion", "MethodNotAllowed":
		return &consoleapi.Error{Status: http.StatusNotFound, Code: "version_unavailable", Message: "The selected version is missing or is a deletion marker."}
	default:
		return normalizeError(err)
	}
}

func featureMutationError(err error) *consoleapi.Error {
	switch minio.ToErrorResponse(err).Code {
	case "BucketAlreadyExists", "BucketAlreadyOwnedByYou":
		return &consoleapi.Error{Status: http.StatusConflict, Code: "bucket_exists", Message: "A bucket already exists with this name."}
	case "BucketNotEmpty":
		return &consoleapi.Error{Status: http.StatusConflict, Code: "bucket_not_empty", Message: "The bucket is not empty. Objects, historical versions and deletion markers must be removed separately."}
	default:
		return mutationFailure(err)
	}
}

// Bucket creation has an SDK region retry outside MaxRetries. An operation owns
// this transport and its fresh signer so even that retry cannot replay a write.
type bucketMutationTransport struct {
	base       http.RoundTripper
	dispatched atomic.Bool
}

func (t *bucketMutationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPut || req.Method == http.MethodDelete {
		if !t.dispatched.CompareAndSwap(false, true) {
			return nil, errors.New("bucket mutation replay refused")
		}
	}
	return t.base.RoundTrip(req)
}

func (c *Client) bucketMutationClient() (*minio.Client, error) {
	opts := c.s3Options
	opts.Transport = &bucketMutationTransport{base: c.s3Transport}
	client, err := minio.New(c.s3Endpoint, &opts)
	if err != nil {
		return nil, configError()
	}
	client.SetAppInfo(c.appName, c.appVersion)
	return client, nil
}

func (c *Client) CreateBucket(ctx context.Context, bucket string) error {
	if err := c.ready(ctx); err != nil {
		return err
	}
	if s3utils.CheckValidBucketNameStrict(bucket) != nil {
		return invalidRequest()
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	client, err := c.bucketMutationClient()
	if err != nil {
		return err
	}
	if err := client.MakeBucket(requestCtx, bucket, minio.MakeBucketOptions{}); err != nil {
		return featureMutationError(err)
	}
	return nil
}

func (c *Client) DeleteBucket(ctx context.Context, bucket string) error {
	if err := c.ready(ctx); err != nil {
		return err
	}
	if s3utils.CheckValidBucketNameStrict(bucket) != nil {
		return invalidRequest()
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	client, err := c.bucketMutationClient()
	if err != nil {
		return err
	}
	// S3 DELETE is the authoritative emptiness check, including concurrent PUTs
	// and history. No force flag, enumerate-and-clear phase, or replay is used.
	if err := client.RemoveBucket(requestCtx, bucket); err != nil {
		return featureMutationError(err)
	}
	return nil
}

type versionCursor struct {
	Bucket  string `json:"b"`
	Key     string `json:"k"`
	Marker  string `json:"m"`
	Version string `json:"v"`
}

type versionXML struct {
	EncodingType        string
	IsTruncated         bool
	NextKeyMarker       string
	NextVersionIDMarker string
	Entries             []consoleapi.VersionEntry
}

func (page *versionXML) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
	if start.Name.Local != "ListVersionsResult" {
		return errors.New("unexpected version page root")
	}
	for {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.EndElement:
			if element.Name == start.Name {
				return nil
			}
		case xml.StartElement:
			var target any
			switch element.Name.Local {
			case "EncodingType":
				target = &page.EncodingType
			case "IsTruncated":
				target = &page.IsTruncated
			case "NextKeyMarker":
				target = &page.NextKeyMarker
			case "NextVersionIdMarker":
				target = &page.NextVersionIDMarker
			case "Version", "DeleteMarker":
				var entry struct {
					Key          string
					VersionID    string `xml:"VersionId"`
					IsLatest     bool
					Size         int64
					ETag         string
					LastModified time.Time
				}
				if err := dec.DecodeElement(&entry, &element); err != nil {
					return err
				}
				page.Entries = append(page.Entries, consoleapi.VersionEntry{Key: entry.Key, VersionID: entry.VersionID, Latest: entry.IsLatest, DeleteMarker: element.Name.Local == "DeleteMarker", Size: entry.Size, ETag: strings.Trim(entry.ETag, "\""), Modified: entry.LastModified})
				continue
			default:
				if err := dec.Skip(); err != nil {
					return err
				}
				continue
			}
			if err := dec.DecodeElement(target, &element); err != nil {
				return err
			}
		}
	}
}

func (c *Client) ListVersions(ctx context.Context, bucket, key, cursor string, limit int) (consoleapi.VersionPage, error) {
	result := consoleapi.VersionPage{Entries: []consoleapi.VersionEntry{}}
	if err := c.ready(ctx); err != nil {
		return result, err
	}
	if !validReference(consoleapi.ObjectRef{Bucket: bucket, Key: key}) || limit < 1 || limit > 1000 || len(cursor) > 16*1024 {
		return result, invalidRequest()
	}
	marker := versionCursor{}
	if cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(data, &marker) != nil || marker.Bucket != bucket || marker.Key != key || marker.Marker == "" || len(marker.Marker) > 16*1024 || !utf8.ValidString(marker.Marker) || strings.ContainsRune(marker.Marker, 0) || marker.Version == "" || !validVersionID(marker.Version) {
			return result, invalidRequest()
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	query := url.Values{"versions": {""}, "prefix": {key}, "encoding-type": {"url"}, "max-keys": {strconv.Itoa(limit)}}
	if cursor != "" {
		query.Set("key-marker", marker.Marker)
		query.Set("version-id-marker", marker.Version)
	}
	// The SDK exposes only a channel that automatically fetches every version
	// page. Presign + the owned transport preserves exact page markers while
	// reusing the SDK signer, identity, region discovery and bucket lookup rules.
	u, err := c.s3.Presign(requestCtx, http.MethodGet, bucket, "", time.Minute, query)
	if err != nil {
		return result, normalizeError(err)
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return result, invalidRequest()
	}
	resp, err := c.s3Transport.RoundTrip(req)
	if err != nil {
		return result, normalizeError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var upstream struct{ Code string }
		_ = xml.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&upstream)
		return result, normalizeError(minio.ErrorResponse{Code: upstream.Code, StatusCode: resp.StatusCode})
	}
	if resp.Header.Get("X-Otterio-Version-Authorization") != "v1" {
		return result, versionAuthorizationUnsupported()
	}
	bounded := &io.LimitedReader{R: resp.Body, N: 4<<20 + 1}
	var page versionXML
	if err := xml.NewDecoder(bounded).Decode(&page); err != nil || bounded.N == 0 || len(page.Entries) > limit || (page.EncodingType != "" && page.EncodingType != "url") {
		return result, normalizeError(errors.New("invalid version page"))
	}
	sawNeighbor := false
	for _, entry := range page.Entries {
		if page.EncodingType == "url" {
			entry.Key, err = url.QueryUnescape(entry.Key)
			if err != nil {
				return result, normalizeError(errors.New("invalid version key encoding"))
			}
		}
		if entry.Key == key {
			if entry.VersionID == "" || !validVersionID(entry.VersionID) || entry.Size < 0 {
				return result, normalizeError(errors.New("invalid version entry"))
			}
			result.Entries = append(result.Entries, entry)
		} else {
			sawNeighbor = true
		}
	}
	if page.EncodingType == "url" {
		page.NextKeyMarker, err = url.QueryUnescape(page.NextKeyMarker)
		if err != nil {
			return result, normalizeError(errors.New("invalid version marker encoding"))
		}
	}
	// Prefix listing may include neighboring keys. Stop after encountering one,
	// but preserve the server's complete opaque marker: OtterIO may append a
	// cache identity to it, so it cannot be compared to the literal object key.
	if page.IsTruncated && !sawNeighbor {
		if page.NextKeyMarker == "" || len(page.NextKeyMarker) > 16*1024 || !utf8.ValidString(page.NextKeyMarker) || strings.ContainsRune(page.NextKeyMarker, 0) || page.NextVersionIDMarker == "" || !validVersionID(page.NextVersionIDMarker) || (cursor != "" && marker.Marker == page.NextKeyMarker && marker.Version == page.NextVersionIDMarker) {
			return result, normalizeError(errors.New("invalid version continuation"))
		}
		data, _ := json.Marshal(versionCursor{Bucket: bucket, Key: key, Marker: page.NextKeyMarker, Version: page.NextVersionIDMarker})
		result.NextCursor = base64.RawURLEncoding.EncodeToString(data)
		if len(result.NextCursor) > 16*1024 {
			return consoleapi.VersionPage{}, normalizeError(errors.New("oversized version continuation"))
		}
	}
	return result, nil
}

func validDownloadName(name string) bool {
	return len(name) <= 255 && utf8.ValidString(name) && !strings.ContainsAny(name, "\x00\r\n/\\") && name != "." && name != ".."
}

func (c *Client) Presign(ctx context.Context, args consoleapi.ShareRequest) (consoleapi.Share, error) {
	if err := c.ready(ctx); err != nil {
		return consoleapi.Share{}, err
	}
	if !validReference(args.ObjectRef) || args.ExpiresSeconds < 1 || args.ExpiresSeconds > 7*24*60*60 || !validDownloadName(args.DownloadName) {
		return consoleapi.Share{}, invalidRequest()
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	// Verify the exact object exists and the selected account can read it before
	// minting a capability. The recipient still needs access to ShareURL.
	if _, err := c.StatReference(requestCtx, args.ObjectRef); err != nil {
		return consoleapi.Share{}, err
	}
	region, err := c.s3.GetBucketLocation(requestCtx, args.Bucket)
	if err != nil {
		return consoleapi.Share{}, normalizeError(err)
	}
	opts := c.s3Options
	opts.Region, opts.Secure = region, c.shareSecure
	signer, err := minio.New(c.shareEndpoint, &opts)
	if err != nil {
		return consoleapi.Share{}, configError()
	}
	query := url.Values{}
	if args.VersionID != "" {
		query.Set("versionId", args.VersionID)
	}
	if args.DownloadName != "" {
		query.Set("response-content-disposition", mime.FormatMediaType("attachment", map[string]string{"filename": args.DownloadName}))
	}
	query.Set("response-content-type", "application/octet-stream")
	u, err := signer.PresignedGetObject(requestCtx, args.Bucket, args.Key, time.Duration(args.ExpiresSeconds)*time.Second, query)
	if err != nil {
		return consoleapi.Share{}, normalizeError(err)
	}
	expires := time.Now().UTC().Add(time.Duration(args.ExpiresSeconds) * time.Second)
	if signedAt, err := time.Parse("20060102T150405Z", u.Query().Get("X-Amz-Date")); err == nil {
		expires = signedAt.Add(time.Duration(args.ExpiresSeconds) * time.Second)
	} else if unix, err := strconv.ParseInt(u.Query().Get("Expires"), 10, 64); err == nil {
		expires = time.Unix(unix, 0).UTC()
	}
	return consoleapi.Share{URL: u.String(), ExpiresAt: expires}, nil
}

// VersionSupported performs a read-only, zero-entry version listing. Neither an
// unversioned bucket nor an empty version list implies lack of server support.
func (c *Client) VersionSupported(ctx context.Context, bucket string) (bool, error) {
	if err := c.ready(ctx); err != nil {
		return false, err
	}
	if !validMutationTarget(bucket, "capability-probe") {
		return false, invalidRequest()
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	query := url.Values{"versions": {""}, "max-keys": {"0"}, "encoding-type": {"url"}}
	u, err := c.s3.Presign(requestCtx, http.MethodGet, bucket, "", time.Minute, query)
	if err != nil {
		return false, normalizeError(err)
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, invalidRequest()
	}
	resp, err := c.s3Transport.RoundTrip(req)
	if err != nil {
		return false, normalizeError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotImplemented || resp.StatusCode == http.StatusMethodNotAllowed {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		var upstream struct{ Code string }
		_ = xml.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&upstream)
		if upstream.Code == "NotImplemented" || upstream.Code == "NotSupported" {
			return false, nil
		}
		return false, normalizeError(minio.ErrorResponse{Code: upstream.Code, StatusCode: resp.StatusCode})
	}
	if resp.Header.Get("X-Otterio-Version-Authorization") != "v1" {
		return false, nil
	}
	var page versionXML
	bounded := &io.LimitedReader{R: resp.Body, N: 64*1024 + 1}
	if xml.NewDecoder(bounded).Decode(&page) != nil || bounded.N == 0 || len(page.Entries) != 0 || page.IsTruncated {
		return false, normalizeError(errors.New("invalid version capability response"))
	}
	return true, nil
}
