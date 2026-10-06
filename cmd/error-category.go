package cmd

import (
	"context"
	"errors"
	"net"

	"github.com/minio/minio-go/v7"
	"github.com/soulteary/otterio/pkg/madmin"
)

// Additive machine-readable fields; original SDK errors and messages remain intact.
func classifyClientError(err error) (string, string) {
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
