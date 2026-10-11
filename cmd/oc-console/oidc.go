// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/soulteary/mc/internal/clienttransport"
	"github.com/soulteary/mc/internal/console"
	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/mc/internal/oidc"
	"github.com/soulteary/mc/internal/storageclient"
)

// Keep identity provider trust independent from storage trust. Fixed-origin
// routing also prevents a future protocol change from widening destination trust.
type oidcTransport struct {
	issuer, storage string
	idp, sts        *http.Transport
}

func (t *oidcTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	origin := r.URL.Scheme + "://" + r.URL.Host
	if origin == t.issuer {
		return t.idp.RoundTrip(r)
	}
	if origin == t.storage {
		return t.sts.RoundTrip(r)
	}
	return nil, errors.New("unexpected OIDC upstream origin")
}
func readOIDCSecret(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	// Windows mode bits cannot establish private ACLs; keep file secrets closed.
	if runtime.GOOS == "windows" {
		return "", errors.New("OIDC client secret files require POSIX permission checks; use a public client on Windows")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return "", errors.New("OIDC client secret requires a private regular file of at most 4096 bytes")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", errors.New("cannot read OIDC client secret file")
	}
	defer f.Close()
	opened, statErr := f.Stat()
	if statErr != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm()&0077 != 0 {
		return "", errors.New("OIDC client secret file changed during opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	secret := strings.TrimSpace(string(data))
	if err != nil || len(data) > 4096 || secret == "" || strings.ContainsAny(secret, "\r\n\x00") {
		return "", errors.New("invalid OIDC client secret file")
	}
	return secret, nil
}
func oidcCallbackURL(publicURL string) string {
	return strings.TrimRight(publicURL, "/") + "/api/oidc/callback"
}

func oidcDataDir(opts options, getenv func(string) string) (string, error) {
	if opts.dataDir != "" {
		return opts.dataDir, nil
	}
	configDir := opts.configDir
	if configDir == "" {
		var err error
		configDir, err = defaultConfigDir(getenv)
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(configDir, "console-data"), nil
}

func oidcClientConfig(opts options) (storageclient.Config, error) {
	// The config directory has already selected preference storage. OIDC
	// still uses fixed endpoints and never loads alias credentials from it.
	opts.configDir = ""
	return nativeClientConfig(opts)
}

func runOIDC(ctx context.Context, opts options, output io.Writer) error {
	var err error
	opts.dataDir, err = oidcDataDir(opts, os.Getenv)
	if err != nil {
		return err
	}
	if opts.sessionTTL < time.Minute || opts.sessionTTL > 24*time.Hour {
		return errors.New("OIDC session lifetime must be between 1m and 24h")
	}
	cfg, err := oidcClientConfig(opts)
	if err != nil {
		return err
	}
	issuer, err := url.Parse(opts.oidcIssuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return errors.New("OIDC requires a fixed HTTPS issuer")
	}
	storage, _ := url.Parse(cfg.S3URL)
	issuerOrigin := issuer.Scheme + "://" + issuer.Host
	storageOrigin := storage.Scheme + "://" + storage.Host
	if issuerOrigin == storageOrigin {
		return errors.New("identity provider and storage must use separate origins")
	}
	secret, err := readOIDCSecret(opts.oidcSecretFile)
	if err != nil {
		return err
	}
	idp := http.DefaultTransport.(*http.Transport).Clone()
	idp.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if opts.oidcCA != "" {
		idp.TLSClientConfig.RootCAs, _, err = clienttransport.LoadCAFile(opts.oidcCA)
		if err != nil {
			return errors.New("cannot load identity provider CA")
		}
	}
	sts := http.DefaultTransport.(*http.Transport).Clone()
	sts.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: cfg.RootCAs}
	defer idp.CloseIdleConnections()
	defer sts.CloseIdleConnections()
	client, err := oidc.New(ctx, oidc.Config{Issuer: opts.oidcIssuer, ClientID: opts.oidcClientID, ClientSecret: secret, RedirectURI: oidcCallbackURL(opts.publicURL), STSEndpoint: cfg.S3URL, Scopes: []string{"offline_access"}, MaxSessionLifetime: opts.sessionTTL, Transport: &oidcTransport{issuer: issuerOrigin, storage: storageOrigin, idp: idp, sts: sts}})
	if err != nil {
		return errors.New("cannot initialize OIDC provider; verify fixed endpoints and client configuration")
	}
	certificate, err := tls.LoadX509KeyPair(opts.tlsCert, opts.tlsKey)
	if err != nil {
		return errors.New("cannot load browser TLS certificate and key")
	}
	connect := func(ctx context.Context, c oidc.Credentials) (consoleapi.Backend, func(), error) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		target := cfg
		target.AccessKey, target.SecretKey, target.SessionToken = c.AccessKey, c.SecretKey, c.SessionToken
		backend, err := storageclient.New(target)
		if err != nil {
			return nil, nil, err
		}
		return backend, backend.Close, nil
	}
	handler, err := console.New(console.Config{OIDC: &console.OIDCConfig{Client: client, StorageTarget: cfg.S3URL + "\x00" + cfg.AdminURL, Connect: connect}, Alias: "storage", BaseURL: opts.publicURL, DataDir: opts.dataDir, SessionTTL: opts.sessionTTL, AllowWrites: opts.allowWrites, MaxUploadSize: opts.maxUploadSize, AllowSharing: opts.allowSharing, MaxArchiveSize: opts.maxArchiveSize, ArchiveDir: opts.archiveDir})
	if err != nil {
		return errors.New("cannot initialize OIDC console")
	}
	defer handler.Close()
	listener, err := net.Listen("tcp", opts.address)
	if err != nil {
		return errors.New("cannot listen on requested address")
	}
	defer listener.Close()
	server := &http.Server{Handler: handler, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	if _, err = fmt.Fprintf(output, "OC console (OIDC login)\nURL: %s\nFixed session lifetime: %s\n", opts.publicURL, opts.sessionTTL); err != nil {
		return errors.New("cannot print startup information")
	}
	done := make(chan error, 1)
	go func() { done <- server.ServeTLS(listener, "", "") }()
	var serveErr error
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = errors.New("OIDC listener stopped unexpectedly")
		}
	case <-ctx.Done():
	}
	_ = handler.Close()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = server.Shutdown(shutdown); err != nil {
		_ = server.Close()
	}
	cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	return errors.Join(serveErr, err, handler.Wait(cleanup))
}
