// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLifecycleTargetProtocolUsesAdminTrustPolicy(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(lifecycleTargetProtocolHeader, "v1")
		_, _ = w.Write([]byte("[]"))
	})
	globalServer := httptest.NewTLSServer(handler)
	defer globalServer.Close()
	// A second certificate represents an independent management endpoint.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "independent admin"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, BasicConstraintsValid: true, IsCA: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	adminServer := httptest.NewUnstartedServer(handler)
	adminServer.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	adminServer.StartTLS()
	defer adminServer.Close()
	adminCA := filepath.Join(t.TempDir(), "independent-admin.pem")
	if err := os.WriteFile(adminCA, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	previous := globalRootCAs
	t.Cleanup(func() { globalRootCAs = previous })
	globalRootCAs = x509.NewCertPool()
	globalRootCAs.AddCert(globalServer.Certificate())
	for _, tc := range []struct {
		name, endpoint, ca string
		wantOK             bool
	}{
		{"default configuration CA", globalServer.URL, "", true},
		{"independent admin CA", adminServer.URL, adminCA, true},
		{"global CA cannot authorize independent admin", adminServer.URL, "", false},
		{"explicit admin CA overrides global CA", globalServer.URL, adminCA, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{HostURL: tc.endpoint, AdminCAFile: tc.ca, AccessKey: "synthetic-access", SecretKey: "synthetic-secret"}
			client, err := NewAdminFactory()(config)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, adminErr := client.ListRemoteTargets(ctx, "source", "ilm")
			protocolErr := checkLifecycleTargetProtocol(ctx, config, "source")
			if (adminErr == nil) != tc.wantOK || (protocolErr == nil) != tc.wantOK {
				t.Fatalf("capability and existing admin trust differ: admin=%v capability=%v want success=%v", adminErr, protocolErr, tc.wantOK)
			}
		})
	}
}

func TestLifecycleTargetProtocolPreflight(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		headers []string
		body    string
		wantOK  bool
	}{
		{"supported", 200, []string{"v1"}, "[]", true},
		{"new bucket", 404, []string{"v1"}, `{"Code":"NoSuchBucketTargets"}`, true},
		{"old server", 200, nil, "[]", false},
		{"future protocol", 200, []string{"v2"}, "[]", false},
		{"duplicate header", 200, []string{"v1", "v1"}, "[]", false},
		{"denied", 403, []string{"v1"}, "denied", false},
		{"uninitialized", 503, []string{"v1"}, "unavailable", false},
		{"body too large", 200, []string{"v1"}, strings.Repeat("x", (4<<20)+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/otterio/admin/v3/list-remote-targets" ||
					r.URL.Query().Get("bucket") != "source" || r.URL.Query().Get("type") != "ilm" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") || r.Header.Get("X-Amz-Security-Token") != "synthetic-token" {
					t.Error("preflight must issue one signed read on the management endpoint")
				}
				for _, value := range tc.headers {
					w.Header().Add(lifecycleTargetProtocolHeader, value)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			err := checkLifecycleTargetProtocol(context.Background(), &Config{HostURL: server.URL, AccessKey: "synthetic-access", SecretKey: "synthetic-secret", SessionToken: "synthetic-token"}, "source")
			if (err == nil) != tc.wantOK || requests != 1 {
				t.Fatalf("preflight success=%v requests=%d; want success=%v, one read", err == nil, requests, tc.wantOK)
			}
		})
	}
}

func TestLifecycleTargetProtocolDoesNotFollowRedirect(t *testing.T) {
	forwarded := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded++
		w.Header().Set(lifecycleTargetProtocolHeader, "v1")
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	if err := checkLifecycleTargetProtocol(context.Background(), &Config{HostURL: server.URL, AccessKey: "synthetic", SecretKey: "synthetic-secret"}, "source"); err == nil || forwarded != 0 {
		t.Fatalf("redirect must fail without forwarding credentials, forwarded=%d error=%v", forwarded, err)
	}
}

func TestLifecycleTargetProtocolHonorsCancellationAndIncompleteBody(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(lifecycleTargetProtocolHeader, "v1")
			if incomplete {
				w.Header().Set("Content-Length", "100")
				_, _ = w.Write([]byte("[]"))
				return
			}
			<-r.Context().Done()
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		err := checkLifecycleTargetProtocol(ctx, &Config{HostURL: server.URL, AccessKey: "synthetic", SecretKey: "synthetic-secret"}, "source")
		cancel()
		server.Close()
		if err == nil {
			t.Fatal("incomplete or canceled response must fail closed")
		}
	}
}
