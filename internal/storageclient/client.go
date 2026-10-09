// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

// Package storageclient implements the console's storage-only API using an
// immutable startup identity. It never resolves local paths or CLI aliases.
package storageclient

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/soulteary/mc/internal/clienttransport"
	"github.com/soulteary/mc/internal/consoleapi"
	minio "github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
	"github.com/soulteary/otterio-sdk/v7/pkg/s3utils"
	"github.com/soulteary/otterio/pkg/madmin"
)

type Config struct {
	S3URL, AdminURL, AccessKey, SecretKey, SessionToken string
	ShareURL                                            string
	API, Path, AppName, AppVersion                      string
	RootCAs, AdminRootCAs                               *x509.CertPool
}

type Client struct {
	s3                  *minio.Client
	admin               *madmin.AdminClient
	adminEndpoint       string
	s3Endpoint          string
	shareEndpoint       string
	shareSecure         bool
	s3Options           minio.Options
	appName, appVersion string
	s3Transport         *http.Transport
	adminTransport      *http.Transport
	metadataTimeout     time.Duration
	closed              atomic.Bool
}

var _ consoleapi.Backend = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	s3URL, err := clienttransport.ValidateAdminEndpoint(cfg.S3URL)
	if err != nil {
		return nil, configError()
	}
	if cfg.AdminURL == "" {
		cfg.AdminURL = s3URL.String()
	}
	adminURL, err := clienttransport.ValidateAdminEndpoint(cfg.AdminURL)
	if err != nil {
		return nil, configError()
	}
	if cfg.ShareURL == "" {
		cfg.ShareURL = s3URL.String()
	}
	shareURL, err := clienttransport.ValidateAdminEndpoint(cfg.ShareURL)
	if err != nil {
		return nil, configError()
	}
	lookup := minio.BucketLookupAuto
	switch strings.ToLower(cfg.Path) {
	case "", "auto":
	case "on":
		lookup = minio.BucketLookupPath
	case "off":
		lookup = minio.BucketLookupDNS
	default:
		return nil, configError()
	}
	s3Credentials := credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken)
	switch strings.ToLower(cfg.API) {
	case "", "s3v4":
	case "s3v2":
		if cfg.SessionToken != "" {
			return nil, configError()
		}
		s3Credentials = credentials.NewStaticV2(cfg.AccessKey, cfg.SecretKey, "")
	default:
		return nil, configError()
	}
	client := &Client{
		s3Endpoint: s3URL.Host, appName: cfg.AppName, appVersion: cfg.AppVersion,
		shareEndpoint: shareURL.Host, shareSecure: shareURL.Scheme == "https",
		adminEndpoint:   adminURL.String(),
		s3Transport:     clienttransport.New(cfg.RootCAs),
		adminTransport:  clienttransport.New(cfg.AdminRootCAs),
		metadataTimeout: 15 * time.Second,
	}
	client.s3Options = minio.Options{
		Creds: s3Credentials, Secure: s3URL.Scheme == "https", BucketLookup: lookup,
		Transport: capabilityTransport{base: client.s3Transport}, MaxRetries: 1,
	}
	client.s3, err = minio.New(client.s3Endpoint, &client.s3Options)
	if err != nil {
		client.Close()
		return nil, configError()
	}
	client.s3.SetAppInfo(cfg.AppName, cfg.AppVersion)
	client.admin, err = madmin.NewWithOptions(adminURL.Host, &madmin.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken),
		Secure: adminURL.Scheme == "https",
	})
	if err != nil {
		client.Close()
		return nil, configError()
	}
	client.admin.SetCustomTransport(clienttransport.Admin{Base: client.adminTransport})
	client.admin.SetAppInfo(cfg.AppName, cfg.AppVersion)
	return client, nil
}

// Close releases only pools owned by this client; no global transport is used.
// The HTTP server owns cancellation of active browser requests and body streams.
func (c *Client) Close() {
	if !c.closed.Swap(true) {
		c.s3Transport.CloseIdleConnections()
		c.adminTransport.CloseIdleConnections()
	}
}

func (c *Client) ready(ctx context.Context) error {
	if c.closed.Load() {
		return &consoleapi.Error{Status: http.StatusServiceUnavailable, Code: "ClientClosed", Message: "The storage connection is closed."}
	}
	if ctx.Err() != nil {
		return normalizeError(ctx.Err())
	}
	return nil
}

func (c *Client) ListBuckets(ctx context.Context) ([]consoleapi.Bucket, error) {
	if err := c.ready(ctx); err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	buckets, err := c.s3.ListBuckets(requestCtx)
	if err != nil {
		return nil, normalizeError(err)
	}
	result := make([]consoleapi.Bucket, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, consoleapi.Bucket{Name: bucket.Name, Created: bucket.CreationDate})
	}
	return result, nil
}

// requestContextTransport is used by one Core only. The pinned Core pagination
// API creates a background context; replace that context at the network boundary
// without changing any shared SDK client or connection pool.
type requestContextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t requestContextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req.Clone(t.ctx))
}

func (c *Client) ListObjects(ctx context.Context, bucket, prefix, cursor string, limit int) (consoleapi.Page, error) {
	return c.listObjects(ctx, bucket, prefix, cursor, limit, "/")
}

func (c *Client) listObjects(ctx context.Context, bucket, prefix, cursor string, limit int, delimiter string) (consoleapi.Page, error) {
	if err := c.ready(ctx); err != nil {
		return consoleapi.Page{}, err
	}
	if limit < 1 || limit > 1000 || s3utils.CheckValidBucketName(bucket) != nil || s3utils.CheckValidObjectNamePrefix(prefix) != nil {
		return consoleapi.Page{}, invalidRequest()
	}
	// Bound the complete XML read, not only the wait for response headers.
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	opts := c.s3Options
	opts.Transport = requestContextTransport{ctx: requestCtx, base: c.s3Transport}
	core, err := minio.NewCore(c.s3Endpoint, &opts)
	if err != nil {
		return consoleapi.Page{}, configError()
	}
	core.SetAppInfo(c.appName, c.appVersion)
	page, err := core.ListObjectsV2(bucket, prefix, "", cursor, delimiter, limit)
	if err != nil {
		return consoleapi.Page{}, normalizeError(err)
	}
	result := consoleapi.Page{Entries: make([]consoleapi.Entry, 0, len(page.CommonPrefixes)+len(page.Contents))}
	for _, prefix := range page.CommonPrefixes {
		result.Entries = append(result.Entries, consoleapi.Entry{Key: prefix.Prefix, IsPrefix: true})
	}
	for _, object := range page.Contents {
		result.Entries = append(result.Entries, consoleapi.Entry{
			Key: object.Key, Size: object.Size, Modified: object.LastModified, ETag: object.ETag,
		})
	}
	if page.IsTruncated {
		result.NextCursor = page.NextContinuationToken
	}
	return result, nil
}

func (c *Client) OpenObject(ctx context.Context, bucket, key string) (consoleapi.Object, error) {
	if err := c.ready(ctx); err != nil {
		return consoleapi.Object{}, err
	}
	if s3utils.CheckValidBucketName(bucket) != nil || s3utils.CheckValidObjectName(key) != nil {
		return consoleapi.Object{}, invalidRequest()
	}
	// GET from the high-level SDK is lazy. HEAD establishes existence first,
	// then Core.GET opens the response before exposing a download to the browser.
	headCtx, cancelHead := context.WithTimeout(ctx, c.metadataTimeout)
	_, err := c.s3.StatObject(headCtx, bucket, key, minio.StatObjectOptions{})
	cancelHead()
	if err != nil {
		return consoleapi.Object{}, normalizeError(err)
	}
	body, info, _, err := (minio.Core{Client: c.s3}).GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		if body != nil {
			_ = body.Close()
		}
		return consoleapi.Object{}, normalizeError(err)
	}
	return consoleapi.Object{Body: body, Size: info.Size, ContentType: info.ContentType, Modified: info.LastModified, ETag: info.ETag}, nil
}

func (c *Client) AccountInfo(ctx context.Context) (consoleapi.Account, error) {
	if err := c.ready(ctx); err != nil {
		return consoleapi.Account{}, err
	}
	// The pinned admin SDK retries network failures. Bound those retries even
	// when a caller supplies a context without a deadline.
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := c.admin.AccountInfo(requestCtx)
	if err != nil {
		return consoleapi.Account{}, normalizeError(err)
	}
	result := consoleapi.Account{Buckets: make([]consoleapi.AccountBucket, 0, len(info.Buckets))}
	for _, bucket := range info.Buckets {
		if bucket.Access.Read || bucket.Access.Write {
			result.Buckets = append(result.Buckets, consoleapi.AccountBucket{
				Name: bucket.Name, Size: bucket.Size, Read: bucket.Access.Read, Write: bucket.Access.Write,
			})
		}
	}
	return result, nil
}

func configError() *consoleapi.Error {
	return &consoleapi.Error{Status: http.StatusBadRequest, Code: "InvalidConfiguration", Message: "The storage connection configuration is invalid."}
}

func invalidRequest() *consoleapi.Error {
	return &consoleapi.Error{Status: http.StatusBadRequest, Code: "InvalidRequest", Message: "The storage request is invalid."}
}

func normalizeError(err error) *consoleapi.Error {
	if errors.Is(err, context.Canceled) {
		return &consoleapi.Error{Status: 499, Code: "Canceled", Message: "The storage request was canceled."}
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return &consoleapi.Error{Status: http.StatusGatewayTimeout, Code: "Timeout", Message: "The storage request timed out."}
	}
	// Neither upstream messages nor arbitrary upstream error codes are exposed.
	code := minio.ToErrorResponse(err).Code
	if code == "" {
		code = madmin.ToErrorResponse(err).Code
	}
	switch code {
	case "PreconditionFailed", "ConditionalRequestConflict":
		return objectExists()
	case "NoSuchBucket", "NoSuchKey", "NoSuchObject", "NotFound":
		return &consoleapi.Error{Status: http.StatusNotFound, Code: "NotFound", Message: "The requested bucket or object was not found."}
	case "AccessDenied", "AllAccessDisabled", "InvalidAccessKeyId", "SignatureDoesNotMatch", "InvalidToken", "ExpiredToken", "TokenRefreshRequired":
		return &consoleapi.Error{Status: http.StatusForbidden, Code: "AccessDenied", Message: "The configured account cannot perform this operation."}
	case "InvalidBucketName", "InvalidObjectName", "InvalidArgument", "InvalidRequest", "InvalidContinuationToken":
		return invalidRequest()
	case "AdminRedirectDisabled":
		return &consoleapi.Error{Status: http.StatusBadGateway, Code: "AdminRedirectDisabled", Message: "The management endpoint redirected the request. Check its configured address."}
	case "NotImplemented", "XMinioAdminNotImplemented":
		return &consoleapi.Error{Status: http.StatusNotImplemented, Code: "NotSupported", Message: "The storage server does not support this operation."}
	case "SlowDown", "ServiceUnavailable":
		return &consoleapi.Error{Status: http.StatusServiceUnavailable, Code: "Unavailable", Message: "The storage server is temporarily unavailable."}
	default:
		return &consoleapi.Error{Status: http.StatusBadGateway, Code: "UpstreamError", Message: "The storage server could not complete this request."}
	}
}
