package cmd

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	"github.com/minio/cli"
	"github.com/soulteary/otterio/pkg/madmin"
	yaml "gopkg.in/yaml.v2"
)

func TestAdminEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{"", "ftp://host", "http://", "http://user:secret@host", "http://host/prefix", "http://host/?token=secret", "http://host/#fragment", "http://host/?", "http://host:invalid"} {
		if _, err := validateAdminEndpoint(endpoint); err == nil {
			t.Errorf("accepted invalid admin endpoint %q", endpoint)
		}
	}
	for _, endpoint := range []string{"http://127.0.0.1:9000", "https://localhost:9001/", "https://[::1]:9001"} {
		if _, err := validateAdminEndpoint(endpoint); err != nil {
			t.Errorf("rejected valid endpoint: %v", err)
		}
	}
}

func TestAdminSettingsPrecedence(t *testing.T) {
	t.Setenv("OC_ADMIN_URL", "http://global:9001")
	t.Setenv("OC_ADMIN_URL_test", "http://alias:9001")
	if actual := adminSetting("http://command:9001", "OC_ADMIN_URL", "test", "http://configured:9001", "http://s3:9000"); actual != "http://command:9001" {
		t.Fatal(actual)
	}
	if actual := adminSetting("", "OC_ADMIN_URL", "test", "http://configured:9001", "http://s3:9000"); actual != "http://alias:9001" {
		t.Fatal(actual)
	}
	t.Setenv("OC_ADMIN_URL_test", "")
	if actual := adminSetting("", "OC_ADMIN_URL", "test", "http://configured:9001", "http://s3:9000"); actual != "http://global:9001" {
		t.Fatal(actual)
	}
	t.Setenv("OC_ADMIN_URL", "")
	if actual := adminSetting("", "OC_ADMIN_URL", "test", "http://configured:9001", "http://s3:9000"); actual != "http://configured:9001" {
		t.Fatal(actual)
	}
	if actual := adminSetting("", "OC_ADMIN_URL", "test", "", "http://s3:9000"); actual != "http://s3:9000" {
		t.Fatal(actual)
	}
}

func TestAdminClientCacheIsolation(t *testing.T) {
	factory := NewAdminFactory()
	base := Config{HostURL: "http://127.0.0.1:9000", AccessKey: "test-access", SecretKey: "test-secret"}
	first, err := factory(&base)
	if err != nil {
		t.Fatal(err)
	}
	again, err := factory(&base)
	if err != nil || first != again {
		t.Fatal("identical client was not reused")
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.HostURL = "https://127.0.0.1:9000" }, func(c *Config) { c.SessionToken = "session" }, func(c *Config) { c.Insecure = true }, func(c *Config) { c.Debug = true }, func(c *Config) { c.SecretKey = "another-secret" }} {
		changed := base
		mutate(&changed)
		other, err := factory(&changed)
		if err != nil || other == first {
			t.Fatalf("client cache mixed transport or credentials: %v", err)
		}
	}
}

func TestAdminRedirectDoesNotForwardCredentials(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer target.Close()
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, status) }))
		client, err := NewAdminFactory()(&Config{HostURL: origin.URL, AccessKey: "test-access", SecretKey: "test-secret"})
		if err != nil {
			t.Fatal(err)
		}
		_, requestErr := client.ServerInfo(context.Background())
		origin.Close()
		if requestErr == nil || !strings.Contains(requestErr.Error(), "redirect refused") {
			t.Fatalf("expected redirect rejection, got %v", requestErr)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("redirected endpoint received an admin request")
	}
}

func adminInfoFixture(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(madmin.InfoMessage{Mode: "server", Servers: []madmin.ServerProperties{{Version: "development"}}})
}

func TestAdminIndependentCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(adminInfoFixture))
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "admin.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(ca, data, 0600); err != nil {
		t.Fatal(err)
	}
	previous := globalRootCAs
	globalRootCAs = x509.NewCertPool()
	defer func() { globalRootCAs = previous }()
	factory := NewAdminFactory()
	client, err := factory(&Config{HostURL: server.URL, AccessKey: "test-access", SecretKey: "test-secret", AdminCAFile: ca})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ServerInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	untrusted, err := factory(&Config{HostURL: server.URL, AccessKey: "test-access", SecretKey: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := untrusted.ServerInfo(ctx); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	if err := os.WriteFile(ca, []byte("invalid PEM"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := factory(&Config{HostURL: server.URL, AdminCAFile: ca}); err == nil {
		t.Fatal("invalid CA accepted from cache")
	}
}

func TestPrometheusSplitTargetAndModes(t *testing.T) {
	cfg := &aliasConfigV10{URL: "https://s3.example:9000", AdminURL: "https://admin.example:9001", AccessKey: "test-access", SecretKey: "test-secret"}
	info := madmin.InfoMessage{Servers: []madmin.ServerProperties{{Version: "development"}}}
	for kind, path := range map[string]string{"cluster": "/otterio/v2/metrics/cluster", "node": "/otterio/v2/metrics/node", "legacy": "/otterio/prometheus/metrics"} {
		output, err := buildPrometheusConfig(cfg, info, kind, false)
		if err != nil {
			t.Fatal(err)
		}
		scrape := output.ScrapeConfigs[0]
		if scrape.MetricsPath != path || scrape.StaticConfigs[0].Targets[0] != "s3.example:9000" || scrape.Scheme != "https" {
			t.Fatalf("wrong scrape target: %+v", scrape)
		}
		token, err := jwtgo.Parse(scrape.BearerToken, func(token *jwtgo.Token) (interface{}, error) { return []byte(cfg.SecretKey), nil })
		if err != nil || !token.Valid {
			t.Fatalf("invalid JWT: %v", err)
		}
		claims := token.Claims.(jwtgo.MapClaims)
		if claims["iss"] != "prometheus" || claims["sub"] != cfg.AccessKey {
			t.Fatal("wrong JWT claims")
		}
	}
	public, err := buildPrometheusConfig(cfg, info, "cluster", true)
	if err != nil || public.ScrapeConfigs[0].BearerToken != "" {
		t.Fatal("public config contains a token")
	}
	if _, err := buildPrometheusConfig(cfg, madmin.InfoMessage{}, "cluster", false); err == nil {
		t.Fatal("empty server info accepted")
	}
	if _, err := buildPrometheusConfig(cfg, info, "unknown", false); err == nil {
		t.Fatal("unknown metrics type accepted")
	}
	cfg.SessionToken = "temporary"
	if _, err := buildPrometheusConfig(cfg, info, "cluster", false); err == nil {
		t.Fatal("session credentials used as a static JWT secret")
	}
	if (PrometheusConfig{}).JSON() != "{}" {
		t.Fatal("empty config JSON failed")
	}
}

func TestAliasAdminSettingsRoundTrip(t *testing.T) {
	original := aliasConfigV10{URL: "http://s3:9000", AdminURL: "https://admin:9001", AdminCAFile: "/tmp/admin.pem", AccessKey: "key", SecretKey: "secret"}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded aliasConfigV10
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != original {
		t.Fatal("admin settings lost during serialization")
	}
	if err := json.Unmarshal([]byte(`{"url":"http://legacy:9000"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	var legacy aliasConfigV10
	if err := json.Unmarshal([]byte(`{"url":"http://legacy:9000"}`), &legacy); err != nil || legacy.AdminURL != "" {
		t.Fatal("legacy config incompatible")
	}
}

func TestAdminCommandOverrideAcrossNestedContexts(t *testing.T) {
	app := cli.NewApp()
	rootFlags := flag.NewFlagSet("root", flag.ContinueOnError)
	rootFlags.String("admin-url", "", "")
	if err := rootFlags.Parse([]string{"--admin-url=http://root:9001"}); err != nil {
		t.Fatal(err)
	}
	root := cli.NewContext(app, rootFlags, nil)
	childFlags := flag.NewFlagSet("admin", flag.ContinueOnError)
	childFlags.String("admin-url", "", "")
	child := cli.NewContext(app, childFlags, root)
	leafFlags := flag.NewFlagSet("info", flag.ContinueOnError)
	leafFlags.String("admin-url", "", "")
	leaf := cli.NewContext(app, leafFlags, child)
	if actual := commandStringOverride(leaf, "admin-url"); actual != "http://root:9001" {
		t.Fatal(actual)
	}
	if err := child.Set("admin-url", "http://child:9001"); err != nil {
		t.Fatal(err)
	}
	// IsSet caches its snapshot; create a fresh context after changing the flag.
	child = cli.NewContext(app, childFlags, root)
	leaf = cli.NewContext(app, leafFlags, child)
	if actual := commandStringOverride(leaf, "admin-url"); actual != "http://child:9001" {
		t.Fatal(actual)
	}
}

func TestAdminBodylessPolicyRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/otterio/admin/v3/set-user-or-group-policy" {
			t.Error("wrong policy request")
		}
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			t.Errorf("empty payload was sent as a stream: %+v", r.TransferEncoding)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := NewAdminFactory()(&Config{HostURL: server.URL, AccessKey: "test-access", SecretKey: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetPolicy(context.Background(), "readonly", "test-user", false); err != nil {
		t.Fatal(err)
	}
}

func TestPrometheusPrivateCAConfiguration(t *testing.T) {
	cfg := &aliasConfigV10{URL: "https://s3.example:9000"}
	info := madmin.InfoMessage{Servers: []madmin.ServerProperties{{Version: "development"}}}
	config, err := buildPrometheusConfig(cfg, info, "node", true)
	if err != nil {
		t.Fatal(err)
	}
	// The file belongs to the Prometheus host, so it need not exist locally.
	if err := setMetricsCA(&config, "/etc/prometheus/s3-ca.pem"); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Scrapes []struct {
			TLS struct {
				CAFile string `yaml:"ca_file"`
			} `yaml:"tls_config"`
		} `yaml:"scrape_configs"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil || len(parsed.Scrapes) != 1 || parsed.Scrapes[0].TLS.CAFile != "/etc/prometheus/s3-ca.pem" {
		t.Fatalf("invalid Prometheus TLS YAML: %s, %v", data, err)
	}
	cfg.URL = "http://s3.example:9000"
	config, err = buildPrometheusConfig(cfg, info, "cluster", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := setMetricsCA(&config, "/etc/prometheus/s3-ca.pem"); err == nil {
		t.Fatal("metrics CA accepted for HTTP")
	}
	if err := setMetricsCA(&config, ""); err != nil || config.ScrapeConfigs[0].TLSConfig != nil {
		t.Fatal("default config changed")
	}
}

func TestConfigValidationIncludesAdminEndpoint(t *testing.T) {
	cfg := aliasConfigV10{URL: "http://s3.example:9000", API: "S3v4"}
	for _, endpoint := range []string{"", "https://admin.example:9001/"} {
		cfg.AdminURL = endpoint
		if valid, errors := validateConfigHost(cfg); !valid {
			t.Fatalf("valid config rejected: %v", errors)
		}
	}
	for _, endpoint := range []string{"https://admin.example/prefix", "http://user:secret@admin.example"} {
		cfg.AdminURL = endpoint
		if valid, _ := validateConfigHost(cfg); valid {
			t.Fatal("invalid admin URL accepted by config validation")
		}
	}
}
