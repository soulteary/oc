package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/minio/cli"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/madmin"
)

func TestDoctorEffectiveAliasConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, ocHost, mcHost, adminURL, adminOverride, scheme, adminScheme string
		separate, invalid                                                  bool
		emptyOC, environmentOnly, online, serverError                      bool
	}{
		{name: "file", scheme: "http", adminScheme: "http"},
		{name: "same admin with root slash", adminURL: "http://file.example/", scheme: "http", adminScheme: "http"},
		{name: "OC overrides file", ocHost: "https://access:secret@env.example", scheme: "https", adminScheme: "https"},
		{name: "MC overrides file", mcHost: "https://access:secret@env.example", scheme: "https", adminScheme: "https"},
		{name: "OC overrides MC", ocHost: "https://access:secret@env.example", mcHost: "http://access:secret@legacy.example", scheme: "https", adminScheme: "https"},
		{name: "stored admin preserved", ocHost: "https://access:secret@env.example", adminURL: "http://admin.example", scheme: "https", adminScheme: "http", separate: true},
		{name: "admin environment override", ocHost: "https://access:secret@env.example", adminURL: "https://admin.example", adminOverride: "http://override.example", scheme: "https", adminScheme: "http", separate: true},
		{name: "invalid OC must not fall back", ocHost: "https://access:secret@env.example/path?token=private", invalid: true},
		{name: "empty OC must not fall back", mcHost: "https://access:secret@legacy.example", emptyOC: true, invalid: true},
		{name: "environment only", ocHost: "https://access:secret@env.example", environmentOnly: true, scheme: "https", adminScheme: "https"},
		{name: "online uses effective HTTPS endpoint", online: true, scheme: "https", adminScheme: "https"},
		{name: "online error stays credential free", online: true, serverError: true, scheme: "https", adminScheme: "https"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// An absent variable differs from an explicitly empty override.
			for _, name := range []string{"OC_HOST_store", "MC_HOST_store", "OC_HOSTS_store", "MC_HOSTS_store"} {
				t.Setenv(name, "")
				if err := os.Unsetenv(name); err != nil {
					t.Fatal(err)
				}
			}
			if test.ocHost != "" {
				t.Setenv("OC_HOST_store", test.ocHost)
			}
			if test.emptyOC {
				t.Setenv("OC_HOST_store", "")
			}
			if test.mcHost != "" {
				t.Setenv("MC_HOST_store", test.mcHost)
			}
			t.Setenv("OC_ADMIN_URL", "")
			t.Setenv("OC_ADMIN_URL_store", test.adminOverride)
			t.Setenv("OC_ADMIN_CA", "")
			t.Setenv("OC_ADMIN_CA_store", "")
			previousLoader, previousOutput := loadMcConfig, color.Output
			previousURL, previousCA := globalAdminURL, globalAdminCA
			previousJSON := globalJSON
			previousInsecure, previousContext := globalInsecure, globalContext
			globalInsecure, globalContext = false, context.Background()
			globalJSON = true
			globalAdminURL, globalAdminCA = "", ""
			cfg := newConfigV10()
			cfg.Aliases["store"] = aliasConfigV10{URL: "http://file.example", AdminURL: test.adminURL, AdminCAFile: "/private/ca.pem"}
			if test.environmentOnly {
				delete(cfg.Aliases, "store")
			}
			requests := 0
			if test.online {
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if r.URL.Path != "/otterio/admin/v3/info" {
						t.Errorf("unexpected request path: %s", r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/json")
					if test.serverError {
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte(`{"Code":"AccessDenied","Message":"secret token at private.example"}`))
						return
					}
					_, _ = w.Write([]byte(`{"servers":[{"version":"test-version"}]}`))
				}))
				t.Cleanup(server.Close)
				globalInsecure = true
				stored := cfg.Aliases["store"]
				stored.AdminCAFile = ""
				cfg.Aliases["store"] = stored
				t.Setenv("OC_HOST_store", strings.Replace(server.URL, "https://", "https://access:secret@", 1))
			}
			loads := 0
			loadMcConfig = func() (*configV10, *probe.Error) { loads++; return cfg, nil }
			previousFactory := s3AdminNew
			factoryCalls := 0
			if test.online {
				factory := NewAdminFactory()
				s3AdminNew = func(config *Config) (*madmin.AdminClient, *probe.Error) {
					factoryCalls++
					if diagnosticScheme(config.HostURL) != test.adminScheme || config.AdminCAFile != "" || config.AccessKey != "access" || config.SecretKey != "secret" {
						t.Fatalf("online connection differs from effective configuration")
					}
					return factory(config)
				}
			}
			var output bytes.Buffer
			color.Output = &output
			t.Cleanup(func() {
				loadMcConfig, color.Output = previousLoader, previousOutput
				globalAdminURL, globalAdminCA = previousURL, previousCA
				globalJSON = previousJSON
				globalInsecure, globalContext = previousInsecure, previousContext
				s3AdminNew = previousFactory
			})
			flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
			flags.Bool("online", test.online, "")
			if err := flags.Parse([]string{"store"}); err != nil {
				t.Fatal(err)
			}
			err := mainDoctor(cli.NewContext(cli.NewApp(), flags, nil))
			if test.invalid {
				if err == nil {
					t.Fatal("invalid environment alias was accepted")
				}
			} else {
				if test.serverError && err == nil {
					t.Fatal("online server failure was accepted")
				}
				if !test.serverError && err != nil {
					t.Fatal(err)
				}
				var report doctorReport
				if err := json.Unmarshal(output.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				wantCA := !test.environmentOnly && !test.online
				if report.S3Scheme != test.scheme || report.AdminScheme != test.adminScheme || report.SeparateAdmin != test.separate || report.CustomAdminCA != wantCA {
					t.Fatalf("unexpected effective configuration: %+v", report)
				}
				if test.online && (loads != 1 || factoryCalls != 1 || requests != 1 || !report.Online) {
					t.Fatalf("online diagnostic did not use one resolved configuration: loads=%d requests=%d report=%+v", loads, requests, report)
				}
				if test.online && !test.serverError && (report.ServerCount != 1 || len(report.ServerVersions) != 1 || report.ServerVersions[0] != "test-version") {
					t.Fatalf("online diagnostic did not query the effective endpoint: requests=%d report=%+v", requests, report)
				}
				if test.serverError && (report.Status != "error" || report.ErrorCode != "AccessDenied" || report.ErrorCategory != "permission") {
					t.Fatalf("unexpected online error report: %+v", report)
				}
			}
			for _, secret := range []string{"access", "secret", "env.example", "private", "token"} {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("diagnostic exposed %q", secret)
				}
				if err != nil && strings.Contains(err.Error(), secret) {
					t.Fatalf("returned error exposed %q", secret)
				}
			}
		})
	}
}

func TestDiagnosticEndpointAllowlist(t *testing.T) {
	for _, endpoint := range []string{
		"https://access:secret@private.example:9001/path?token=session&signature=signed#fragment",
		"http://access:secret@localhost:9000", "invalid://secret", "%secret",
	} {
		report := doctorReport{S3Scheme: diagnosticScheme(endpoint), AdminScheme: diagnosticScheme(endpoint)}
		for _, secret := range []string{"access", "secret", "private.example", "session", "signed", "fragment"} {
			if strings.Contains(report.JSON(), secret) {
				t.Fatalf("diagnostic exposed %q", secret)
			}
		}
	}
}
