// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

// Package clienttransport contains protocol transport rules shared by the CLI
// and the console. It has no process configuration or request-specific state.
package clienttransport

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ValidateAdminEndpoint accepts only management root URLs. Signed requests must
// never be redirected to discover their management host.
func ValidateAdminEndpoint(endpoint string) (*url.URL, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("admin endpoint must be an absolute http or https URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(endpoint, "#") || u.Opaque != "" {
		return nil, fmt.Errorf("admin endpoint must not contain credentials, query parameters or fragments")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("admin endpoint path prefixes are unsupported; expose /otterio/admin on the configured host")
	}
	if err := ValidateEndpointHost(u); err != nil {
		return nil, err
	}
	u.Path, u.RawPath = "", ""
	return u, nil
}

func ValidateEndpointHost(u *url.URL) error {
	if u == nil || u.Hostname() == "" || strings.ContainsAny(u.Host, " \t\r\n") {
		return fmt.Errorf("endpoint contains an invalid host")
	}
	if strings.HasPrefix(u.Host, "[") || strings.Count(u.Host, ":") > 1 {
		address, err := netip.ParseAddr(u.Hostname())
		if err != nil || !address.Is6() || !strings.HasPrefix(u.Host, "[") {
			return fmt.Errorf("endpoint contains an invalid IPv6 host")
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("endpoint contains an invalid port")
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return fmt.Errorf("endpoint contains an invalid port")
		}
	}
	return nil
}

// LoadCAFile adds a PEM file to system trust, independently of any S3 trust pool.
// The fingerprint lets a caller identify a rotated CA without using its path.
func LoadCAFile(path string) (*x509.CertPool, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read admin CA: %w", err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(data) {
		return nil, "", fmt.Errorf("admin CA file contains no valid PEM certificates")
	}
	hash := sha256.Sum256(data)
	return roots, hex.EncodeToString(hash[:]), nil
}

// New owns a connection pool and takes a snapshot of its trust configuration.
func New(roots *x509.CertPool) *http.Transport {
	if roots != nil {
		roots = roots.Clone()
	}
	return &http.Transport{
		Proxy:        http.ProxyFromEnvironment,
		DialContext:  (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 15 * time.Second}).DialContext,
		MaxIdleConns: 64, MaxIdleConnsPerHost: 16,
		IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
		ExpectContinueTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		TLSClientConfig:    &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DisableCompression: true,
	}
}

// NormalizeAdminRequest preserves the signed empty-payload distinction required
// by OtterIO's HTTP bridge. Nonempty and differently signed bodies are untouched.
func NormalizeAdminRequest(req *http.Request) *http.Request {
	if req.ContentLength == 0 && req.Body != nil && req.Body != http.NoBody &&
		req.Header.Get("X-Amz-Content-Sha256") == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		copied := req.Clone(req.Context())
		_ = req.Body.Close()
		copied.Body, copied.GetBody, copied.TransferEncoding = http.NoBody, nil, nil
		return copied
	}
	return req
}

// NormalizeAdminResponse prevents an SDK-owned http.Client from following a
// redirect with credentials or replaying an encrypted IAM/configuration body.
func NormalizeAdminResponse(resp *http.Response, err error) (*http.Response, error) {
	if err != nil || resp == nil || resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return resp, err
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	copied := *resp
	copied.StatusCode, copied.Status = http.StatusForbidden, "403 Forbidden"
	copied.Header = make(http.Header)
	copied.Header.Set("Content-Type", "application/json")
	body := `{"Code":"AdminRedirectDisabled","Message":"admin endpoint redirect refused; configure the management URL explicitly"}`
	copied.Body = io.NopCloser(strings.NewReader(body))
	copied.ContentLength = int64(len(body))
	return &copied, nil
}

type Admin struct{ Base http.RoundTripper }

func (t Admin) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.Base.RoundTrip(NormalizeAdminRequest(req))
	return NormalizeAdminResponse(resp, err)
}

func (t Admin) CloseIdleConnections() {
	if closer, ok := t.Base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
