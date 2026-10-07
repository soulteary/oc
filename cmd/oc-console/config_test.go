// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureConfig(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(map[string]interface{}{
		"version": "10", "aliases": map[string]aliasConfig{
			"store": {URL: "https://s3.example", AdminURL: "https://admin.example", AccessKey: "private-access", SecretKey: "private-secret", SessionToken: "private-session", API: "S3v4", Path: "on"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, data
}

func TestConfigSelectionReadOnlyAndPrecedence(t *testing.T) {
	dir, original := fixtureConfig(t)
	env := map[string]string{"OC_ADMIN_URL": "https://global.example", "OC_ADMIN_URL_store": "https://alias.example"}
	getenv := func(key string) string { return env[key] }
	opts := options{configDir: dir, alias: "store"}
	cfg, err := loadClientConfig(opts, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3URL != "https://s3.example" || cfg.AdminURL != "https://alias.example" || cfg.SessionToken != "private-session" || cfg.Path != "on" {
		t.Fatalf("wrong selected configuration")
	}
	opts.adminURL = "https://flag.example"
	cfg, err = loadClientConfig(opts, getenv)
	if err != nil || cfg.AdminURL != opts.adminURL {
		t.Fatalf("flag precedence: %v", err)
	}
	opts.adminURL = ""
	delete(env, "OC_ADMIN_URL_store")
	cfg, err = loadClientConfig(opts, getenv)
	if err != nil || cfg.AdminURL != env["OC_ADMIN_URL"] {
		t.Fatalf("global precedence: %v", err)
	}
	delete(env, "OC_ADMIN_URL")
	cfg, err = loadClientConfig(opts, getenv)
	if err != nil || cfg.AdminURL != "https://admin.example" {
		t.Fatalf("stored precedence: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("configuration changed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("loader created files")
	}
}

func TestConfigurationErrorsDoNotDiscloseCredentials(t *testing.T) {
	dir, _ := fixtureConfig(t)
	for _, alias := range []string{"", "absent", "private-secret\n"} {
		_, err := loadClientConfig(options{configDir: dir, alias: alias}, func(string) string { return "" })
		if err == nil {
			t.Fatal("expected selection error")
		}
		for _, secret := range []string{"private-access", "private-secret", "private-session"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatal("error discloses input")
			}
		}
	}
	_, err := loadClientConfig(options{configDir: dir, alias: "store"}, func(key string) string {
		if key == "OC_HOST_store" {
			return "https://private-access:private-secret@override.example"
		}
		return ""
	})
	if err == nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatal("host override must fail without disclosure")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"version":"9","secret":"private-secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = loadClientConfig(options{configDir: dir, alias: "store"}, func(string) string { return "" })
	if err == nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatal("invalid version accepted or disclosed")
	}
}

func writeTestCA(t *testing.T, dir, name string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIndependentConsoleCATrust(t *testing.T) {
	dir, _ := fixtureConfig(t)
	s3CA := writeTestCA(t, dir, "s3-only")
	adminCA := writeTestCA(t, dir, "admin-only")
	cfg, err := loadClientConfig(options{configDir: dir, alias: "store", s3CA: s3CA, adminCA: adminCA}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs.Equal(cfg.AdminRootCAs) {
		t.Fatal("independent CA pools were merged")
	}
	shared, err := loadClientConfig(options{configDir: dir, alias: "store", s3CA: s3CA}, func(string) string { return "" })
	if err != nil || !shared.RootCAs.Equal(shared.AdminRootCAs) {
		t.Fatalf("CLI-compatible CA fallback: %v", err)
	}
}

func TestLoopbackOnlyListenAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:9090", "127.0.0.2:0", "[::1]:9090"} {
		if err := validateListenAddress(address); err != nil {
			t.Fatalf("%s: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:9090", ":9090", "localhost:9090", "192.168.1.2:9090", "[fe80::1%lo0]:9090", "127.0.0.1:-1", "127.0.0.1:65536", "127.0.0.1"} {
		if validateListenAddress(address) == nil {
			t.Fatalf("accepted unsafe address %q", address)
		}
	}
}

func TestConfigDirectoryAndInformationalFlags(t *testing.T) {
	env := map[string]string{"OC_CONFIG_DIR": "/oc", "MC_CONFIG_DIR": "/mc"}
	getenv := func(key string) string { return env[key] }
	dir, err := defaultConfigDir(getenv)
	if err != nil || dir != "/oc" {
		t.Fatal("OC_CONFIG_DIR precedence")
	}
	delete(env, "OC_CONFIG_DIR")
	dir, err = defaultConfigDir(getenv)
	if err != nil || dir != "/mc" {
		t.Fatal("MC_CONFIG_DIR fallback")
	}
	for _, option := range []string{"--help", "--version"} {
		var output bytes.Buffer
		if err := run(context.Background(), []string{option}, &output, &output); err != nil || output.Len() == 0 {
			t.Fatalf("%s: %v", option, err)
		}
	}
}
