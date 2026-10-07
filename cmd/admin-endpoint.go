package cmd

import (
	"crypto/x509"
	"net/http"
	"net/url"
	"os"

	"github.com/soulteary/mc/internal/clienttransport"
)

func validateAdminEndpoint(endpoint string) (*url.URL, error) {
	return clienttransport.ValidateAdminEndpoint(endpoint)
}

// Keep S3 import and management endpoints consistent about host syntax and ports.
func validateEndpointHost(u *url.URL) error {
	return clienttransport.ValidateEndpointHost(u)
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
	return clienttransport.LoadCAFile(path)
}

// The SDK owns its http.Client; intercept redirects before it can forward a
// signed request or replay an encrypted IAM/config body to a different host.
type adminNoRedirectTransport struct{ http.RoundTripper }

func (t adminNoRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = clienttransport.NormalizeAdminRequest(req)
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
	return clienttransport.NormalizeAdminResponse(resp, err)
}
