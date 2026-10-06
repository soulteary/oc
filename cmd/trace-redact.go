package cmd

import (
	"net/http"
	"net/url"
	"strings"
)

const traceRedacted = "REDACTED"

func sensitiveTraceField(key string) bool {
	switch strings.ToLower(key) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie",
		"x-amz-security-token", "x-amz-signature", "x-amz-credential",
		"awsaccesskeyid", "signature", "securitytoken", "token", "access_token",
		"x-amz-server-side-encryption-customer-key":
		return true
	}
	return false
}

func redactTraceURL(original *url.URL) *url.URL {
	if original == nil {
		return nil
	}
	copied := *original
	if copied.User != nil {
		copied.User = url.User(traceRedacted)
	}
	query := copied.Query()
	for key := range query {
		if sensitiveTraceField(key) {
			query.Set(key, traceRedacted)
		}
	}
	copied.RawQuery = query.Encode()
	return &copied
}

func redactTraceHeaders(original http.Header) http.Header {
	headers := original.Clone()
	for key := range headers {
		if sensitiveTraceField(key) {
			headers.Set(key, traceRedacted)
		}
	}
	if location := headers.Get("Location"); location != "" {
		if u, err := url.Parse(location); err == nil {
			headers.Set("Location", redactTraceURL(u).String())
		}
	}
	return headers
}

func redactTraceRequest(req *http.Request) *http.Request {
	copied := req.Clone(req.Context())
	copied.URL = redactTraceURL(req.URL)
	copied.Header = redactTraceHeaders(req.Header)
	return copied
}

func redactTraceResponse(resp *http.Response) *http.Response {
	copied := *resp
	copied.Header = redactTraceHeaders(resp.Header)
	if resp.Request != nil {
		copied.Request = redactTraceRequest(resp.Request)
	}
	return &copied
}
