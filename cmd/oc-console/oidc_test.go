// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package main

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOIDCSecretFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secret")
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(p, []byte("example-secret"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readOIDCSecret(p); err == nil {
			t.Fatal("secret file accepted without private permission verification")
		}
		if secret, err := readOIDCSecret(""); err != nil || secret != "" {
			t.Fatal("public client rejected")
		}
		return
	}
	for _, tc := range []struct {
		data string
		mode os.FileMode
		ok   bool
	}{{"example-secret\n", 0600, true}, {"example-secret", 0644, false}, {"", 0600, false}, {"first\nsecond", 0600, false}} {
		if err := os.WriteFile(p, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, tc.mode); err != nil {
			t.Fatal(err)
		}
		_, err := readOIDCSecret(p)
		if (err == nil) != tc.ok {
			t.Fatal("unexpected secret validation result")
		}
	}
	link := p + "-link"
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readOIDCSecret(link); err == nil {
		t.Fatal("symlink accepted")
	}
}
func TestOIDCTransportRejectsOtherOrigins(t *testing.T) {
	transport := &oidcTransport{issuer: "https://idp.example:443", storage: "https://storage.example:443"}
	for _, raw := range []string{"https://attacker.example/", "http://idp.example/", "https://idp.example.attacker/"} {
		u, _ := url.Parse(raw)
		if _, err := transport.RoundTrip(&http.Request{URL: u}); err == nil {
			t.Fatal("unexpected origin accepted")
		}
	}
}

func TestOIDCCallbackRootURL(t *testing.T) {
	for _, root := range []string{"https://console.example", "https://console.example/"} {
		if got := oidcCallbackURL(root); got != "https://console.example/api/oidc/callback" {
			t.Fatal("incorrect callback root")
		}
	}
}

func TestOIDCDataDirectoryDefault(t *testing.T) {
	getenv := func(key string) string {
		if key == "OC_CONFIG_DIR" {
			return filepath.Join("profile", "oc")
		}
		return ""
	}
	for _, tc := range []struct {
		opts options
		want string
	}{
		{options{}, filepath.Join("profile", "oc", "console-data")},
		{options{configDir: "custom"}, filepath.Join("custom", "console-data")},
		{options{configDir: "custom", dataDir: "explicit"}, "explicit"},
	} {
		got, err := oidcDataDir(tc.opts, getenv)
		if err != nil || got != tc.want {
			t.Fatalf("preference directory %q, want %q: %v", got, tc.want, err)
		}
	}
}

func TestOIDCFixedEndpointsAcceptPreferenceConfigDirectory(t *testing.T) {
	opts := options{authMode: "oidc", configDir: "custom", s3URL: "https://s3.example", adminURL: "https://admin.example", publicURL: "https://console.example", address: "127.0.0.1:9090", tlsCert: "public.crt", tlsKey: "private.key"}
	cfg, err := oidcClientConfig(opts)
	if err != nil || cfg.S3URL != opts.s3URL || cfg.AccessKey != "" || cfg.SecretKey != "" {
		t.Fatalf("OIDC fixed endpoints rejected preference directory: %v", err)
	}
	if opts.configDir != "custom" {
		t.Fatal("preference directory lost")
	}
	opts.alias = "startup-root"
	if _, err := oidcClientConfig(opts); err == nil {
		t.Fatal("OIDC inherited alias credentials")
	}
}

func TestOIDCOriginEquivalentSpellings(t *testing.T) {
	for _, pair := range [][2]string{
		{"https://idp.example", "https://idp.example:443"},
		{"https://idp.example:0443", "https://idp.example"},
		{"https://bücher.example", "https://xn--bcher-kva.example"},
		{"https://IDP.example", "https://idp.example."},
		{"https://[::1]", "https://[0:0:0:0:0:0:0:1]:443"},
	} {
		a, _ := url.Parse(pair[0])
		b, _ := url.Parse(pair[1])
		if oidcOrigin(a) != oidcOrigin(b) {
			t.Fatalf("equivalent origins differ: %v", pair)
		}
	}
	a, _ := url.Parse("https://idp.example:444")
	b, _ := url.Parse("https://idp.example")
	if oidcOrigin(a) == oidcOrigin(b) {
		t.Fatal("different ports share trust")
	}
}
