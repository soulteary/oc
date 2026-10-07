package cmd

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func validateAdminEndpoint(endpoint string) (*url.URL, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("admin endpoint must be an absolute http or https URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("admin endpoint must not contain credentials, query parameters or fragments")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("admin endpoint path prefixes are unsupported; expose /otterio/admin on the configured host")
	}
	u.Path = ""
	u.RawPath = ""
	return u, nil
}

func adminSetting(command, envName, alias, configured, fallback string) string {
	if command != "" {
		return command
	}
	if value := os.Getenv(envName + "_" + alias); value != "" {
		return value
	}
	if value := os.Getenv(envName); value != "" {
		return value
	}
	if configured != "" {
		return configured
	}
	return fallback
}

func resolveAdminSettings(alias string, cfg *aliasConfigV10) (string, string) {
	return adminSetting(globalAdminURL, "OC_ADMIN_URL", alias, cfg.AdminURL, cfg.URL),
		adminSetting(globalAdminCA, "OC_ADMIN_CA", alias, cfg.AdminCAFile, "")
}

func loadAdminCAs(path string) (*x509.CertPool, string, error) {
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

// The SDK owns its http.Client; intercept redirects before it can forward a
// signed request or replay an encrypted IAM/config body to a different host.
type adminNoRedirectTransport struct{ http.RoundTripper }

func (t adminNoRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// The pinned SDK wraps even an empty payload in a non-nil Reader. For
	// PUT/POST net/http then emits an unknown-length chunked stream. OtterIO's
	// HTTP bridge expects a genuinely bodyless request for these admin calls.
	// Normalize only a zero-length payload signed with the empty SHA-256.
	if req.ContentLength == 0 && req.Body != nil && req.Body != http.NoBody &&
		req.Header.Get("X-Amz-Content-Sha256") == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		copied := req.Clone(req.Context())
		_ = req.Body.Close()
		copied.Body = http.NoBody
		copied.GetBody = nil
		copied.TransferEncoding = nil
		req = copied
	}
	stream, _ := req.Context().Value(adminStreamErrorsKey{}).(adminResponseStream)
	if stream != nil {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		req = req.Clone(stream.ioContext())
	}
	resp, err := t.RoundTripper.RoundTrip(req)
	if err != nil {
		if stream != nil {
			stream.fail(err)
		}
	}
	if err == nil && resp.StatusCode == http.StatusOK {
		if stream != nil {
			resp.Body = stream.decode(resp.Body)
		}
	}
	if err != nil || resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return resp, err
	}
	_ = resp.Body.Close()
	copied := *resp
	copied.StatusCode = http.StatusForbidden
	copied.Status = "403 Forbidden"
	copied.Header = make(http.Header)
	copied.Header.Set("Content-Type", "application/json")
	body := `{"Code":"AdminRedirectDisabled","Message":"admin endpoint redirect refused; configure the management URL explicitly"}`
	copied.Body = io.NopCloser(strings.NewReader(body))
	copied.ContentLength = int64(len(body))
	return &copied, nil
}
