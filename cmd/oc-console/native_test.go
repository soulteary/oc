// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/mc/internal/console"
	"github.com/soulteary/mc/internal/storageclient"
)

func TestNativeConfigurationRejectsStartupCredentialsAndInsecureEndpoints(t *testing.T) {
	base := options{authMode: "native", s3URL: "https://s3.example", adminURL: "https://admin.example", publicURL: "https://console.example", address: "0.0.0.0:9090", tlsCert: "public.crt", tlsKey: "private.key"}
	for _, change := range []func(*options){
		func(o *options) { o.alias = "root-alias" }, func(o *options) { o.configDir = "/config" },
		func(o *options) { o.containerListen = true }, func(o *options) { o.allowWrites = true },
		func(o *options) { o.publicURL = "http://console.example" }, func(o *options) { o.publicURL = "https://0.0.0.0:9090" },
		func(o *options) { o.s3URL = "http://s3.example" }, func(o *options) { o.adminURL = "http://admin.example" },
		func(o *options) { o.s3URL = "https://root:secret@s3.example" }, func(o *options) { o.adminURL = "https://admin.example/prefix" },
		func(o *options) { o.tlsKey = "" }, func(o *options) { o.address = "console.example:9090" },
		func(o *options) { o.allowSharing = true },
	} {
		opts := base
		change(&opts)
		if _, err := nativeClientConfig(opts); err == nil {
			t.Fatal("unsafe native options accepted")
		}
	}
	cfg, err := nativeClientConfig(base)
	if err != nil || cfg.AccessKey != "" || cfg.SecretKey != "" || cfg.SessionToken != "" || cfg.S3URL != "https://s3.example" || cfg.AdminURL != "https://admin.example" {
		t.Fatalf("fixed native target config invalid: %v", err)
	}
}

func TestNativeAuthenticatorUsesSubmittedCredentialsAndStableTargetIdentity(t *testing.T) {
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/otterio/admin/v3/self-credentials" || !strings.Contains(r.Header.Get("Authorization"), "Credential=alice/") || r.Header.Get("X-Amz-Security-Token") != "" {
			t.Error("native login inherited startup credentials or listed buckets")
		}
		w.Header().Set("X-Otterio-Self-Credentials", "v1")
		fmt.Fprint(w, `{"kind":"iam","status":"enabled"}`)
	}))
	defer upstream.Close()
	trust := upstream.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	auth := nativeAuthenticator(storageclient.Config{S3URL: upstream.URL, AdminURL: upstream.URL, RootCAs: trust, AdminRootCAs: trust, AccessKey: "startup-root", SecretKey: "startup-secret", SessionToken: "startup-token"})
	first, err := auth(context.Background(), console.NativeCredentials{AccessKey: "alice", SecretKey: "first-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Cleanup()
	second, err := auth(context.Background(), console.NativeCredentials{AccessKey: "alice", SecretKey: "changed-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Cleanup()
	if first.Backend == second.Backend || first.Identity != second.Identity || len(first.Identity) != 64 || calls != 2 {
		t.Fatal("native connections or preference identity were not independent/stable")
	}
}
