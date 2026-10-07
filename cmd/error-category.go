package cmd

import (
	"context"
	"errors"
	"net"

	"github.com/minio/minio-go/v7"
	"github.com/soulteary/otterio/pkg/madmin"
)

var errWatchStreamClosed = errors.New("watch notification stream closed unexpectedly")

// Additive machine-readable fields; original SDK errors and messages remain intact.
func classifyClientError(err error) (string, string) {
	if errors.Is(err, errConsoleStreamClosed) {
		return "ConsoleStreamClosed", "stream"
	}
	if errors.Is(err, errWatchStreamClosed) {
		return "WatchStreamClosed", "watch"
	}
	var unsupported APINotImplemented
	var unsupportedPointer *APINotImplemented
	if errors.As(err, &unsupported) || errors.As(err, &unsupportedPointer) {
		return "NotImplemented", "unsupported"
	}
	var overflow fsWatchOverflow
	if errors.As(err, &overflow) {
		if overflow.native {
			return "WatchEventsLost", "watch"
		}
		return "WatchQueueSaturated", "watch"
	}
	var admin madmin.ErrorResponse
	var s3 minio.ErrorResponse
	code := ""
	if errors.As(err, &admin) {
		code = admin.Code
	} else if errors.As(err, &s3) {
		code = s3.Code
	}
	switch code {
	case "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken", "InvalidToken", "AccessKeyDisabled":
		return code, "authentication"
	case "AccessDenied", "AllAccessDisabled", "AdminAccessDenied":
		return code, "permission"
	case "NotImplemented", "XNotImplemented", "UnsupportedOperation", "MethodNotAllowed":
		return code, "unsupported"
	case "NoSuchBucket", "NoSuchKey", "NoSuchVersion", "NoSuchUser", "NoSuchGroup":
		return code, "not_found"
	case "AdminRedirectDisabled":
		return code, "endpoint"
	}
	if errors.Is(err, context.Canceled) {
		return code, "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return code, "timeout"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return code, "timeout"
		}
		return code, "network"
	}
	return code, "other"
}
