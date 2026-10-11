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
	transport := &oidcTransport{issuer: "https://idp.example", storage: "https://storage.example"}
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
