// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/soulteary/mc/internal/clienttransport"
	"github.com/soulteary/mc/internal/console"
	"github.com/soulteary/mc/internal/storageclient"
)

func nativeClientConfig(opts options) (storageclient.Config, error) {
	var cfg storageclient.Config
	if opts.alias != "" || opts.configDir != "" || opts.containerListen {
		return cfg, errors.New("native login uses fixed endpoints; --alias, --config-dir and --container-listen are local-mode options")
	}
	if opts.allowSharing && opts.shareURL == "" {
		return cfg, errors.New("native sharing requires an explicit HTTPS --share-url")
	}
	if opts.allowWrites {
		return cfg, errors.New("native login currently supports read-only storage access; shared writes require a later migration phase")
	}
	if opts.tlsCert == "" || opts.tlsKey == "" {
		return cfg, errors.New("native login requires --tls-cert and --tls-key")
	}
	public, err := clienttransport.ValidateAdminEndpoint(opts.publicURL)
	if err != nil || public.Scheme != "https" {
		return cfg, errors.New("native login requires an HTTPS --public-url")
	}
	if ip := net.ParseIP(public.Hostname()); ip != nil && ip.IsUnspecified() {
		return cfg, errors.New("--public-url must name the browser-facing host")
	}
	host, port, err := net.SplitHostPort(opts.address)
	if err != nil {
		return cfg, errors.New("--address must be a literal IP and port")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return cfg, errors.New("--address must be a literal IP and port")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 0 || number > 65535 {
		return cfg, errors.New("invalid native listen port")
	}
	s3, err := clienttransport.ValidateAdminEndpoint(opts.s3URL)
	if err != nil || s3.Scheme != "https" {
		return cfg, errors.New("native login requires a fixed HTTPS --s3-url")
	}
	adminURL := opts.adminURL
	if adminURL == "" {
		adminURL = s3.String()
	}
	admin, err := clienttransport.ValidateAdminEndpoint(adminURL)
	if err != nil || admin.Scheme != "https" {
		return cfg, errors.New("native login requires an HTTPS management endpoint")
	}
	cfg = storageclient.Config{S3URL: s3.String(), AdminURL: admin.String(), ShareURL: opts.shareURL, API: "S3v4", Path: "auto", AppName: "oc-console", AppVersion: version}
	if opts.s3CA != "" {
		cfg.RootCAs, _, err = clienttransport.LoadCAFile(opts.s3CA)
		if err != nil {
			return storageclient.Config{}, errors.New("cannot load --s3-ca PEM certificates")
		}
	}
	cfg.AdminRootCAs = cfg.RootCAs
	if opts.adminCA != "" {
		cfg.AdminRootCAs, _, err = clienttransport.LoadCAFile(opts.adminCA)
		if err != nil {
			return storageclient.Config{}, errors.New("cannot load --admin-ca PEM certificates")
		}
	}
	if opts.shareURL != "" {
		share, err := clienttransport.ValidateAdminEndpoint(opts.shareURL)
		if err != nil || share.Scheme != "https" {
			return storageclient.Config{}, errors.New("native sharing requires an HTTPS --share-url")
		}
	}
	return cfg, nil
}

func nativeAuthenticator(target storageclient.Config) func(context.Context, console.NativeCredentials) (console.NativeConnection, error) {
	// This configuration has no startup identity. Every client uses only the
	// submitted user's immutable credentials and the administrator's fixed target.
	target.AccessKey, target.SecretKey, target.SessionToken = "", "", ""
	return func(ctx context.Context, credentials console.NativeCredentials) (console.NativeConnection, error) {
		cfg := target
		cfg.AccessKey, cfg.SecretKey = credentials.AccessKey, credentials.SecretKey
		client, err := storageclient.New(cfg)
		cfg.SecretKey, credentials.SecretKey = "", ""
		if err != nil {
			return console.NativeConnection{}, err
		}
		if err := client.AuthenticateNative(ctx); err != nil {
			client.Close()
			return console.NativeConnection{}, err
		}
		identity := fmt.Sprintf("%x", sha256.Sum256([]byte(target.S3URL+"\x00"+target.AdminURL+"\x00native\x00"+credentials.AccessKey)))
		return console.NativeConnection{Backend: client, Cleanup: client.Close, Identity: identity}, nil
	}
}

func runNative(ctx context.Context, opts options, output io.Writer) error {
	cfg, err := nativeClientConfig(opts)
	if err != nil {
		return err
	}
	certificate, err := tls.LoadX509KeyPair(opts.tlsCert, opts.tlsKey)
	if err != nil {
		return errors.New("cannot load browser TLS certificate and key")
	}
	handler, err := console.New(console.Config{NativeLogin: nativeAuthenticator(cfg), Alias: "storage", BaseURL: opts.publicURL, DataDir: opts.dataDir, SessionTTL: 30 * time.Minute, AllowSharing: opts.allowSharing, MaxArchiveSize: opts.maxArchiveSize, ArchiveDir: opts.archiveDir})
	if err != nil {
		return errors.New("cannot initialize native HTTPS console")
	}
	defer handler.Close()
	listener, err := net.Listen("tcp", opts.address)
	if err != nil {
		return errors.New("cannot listen on the requested address")
	}
	defer listener.Close()
	server := &http.Server{Handler: handler, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	if _, err := fmt.Fprintf(output, "OC console (native IAM login, read-only)\nURL: %s\nSessions expire after 30 minutes. Each browser signs in with its own IAM access key and secret.\n", opts.publicURL); err != nil {
		return errors.New("cannot print console startup information")
	}
	serveError := make(chan error, 1)
	go func() { serveError <- server.ServeTLS(listener, "", "") }()
	var listenerError error
	select {
	case err := <-serveError:
		if !errors.Is(err, http.ErrServerClosed) {
			listenerError = errors.New("native console listener stopped unexpectedly")
		}
	case <-ctx.Done():
	}
	_ = handler.Close()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownError := server.Shutdown(shutdownCtx)
	cancelShutdown()
	if shutdownError != nil {
		_ = server.Close()
		shutdownError = errors.New("native console HTTP shutdown exceeded five seconds")
	}
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
	cleanupError := handler.Wait(cleanupCtx)
	cancelCleanup()
	if cleanupError != nil {
		cleanupError = errors.New("native console clients did not finish cleanup within five seconds")
	}
	return errors.Join(listenerError, shutdownError, cleanupError)
}
