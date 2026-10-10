// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

// oc-console provides local alias access or independent native IAM login over HTTPS.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/soulteary/mc/internal/console"
	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/mc/internal/storageclient"
)

var version = "development"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "oc-console:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("oc-console", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	var opts options
	flags.StringVar(&opts.authMode, "auth-mode", "local", "local login code or native IAM user login (read-only HTTPS)")
	flags.StringVar(&opts.s3URL, "s3-url", "", "fixed HTTPS storage root URL for native login")
	flags.StringVar(&opts.tlsCert, "tls-cert", "", "browser HTTPS certificate PEM for native login")
	flags.StringVar(&opts.tlsKey, "tls-key", "", "browser HTTPS private key PEM for native login")
	flags.StringVar(&opts.configDir, "config-dir", "", "OC configuration directory (default OC_CONFIG_DIR, MC_CONFIG_DIR, or user profile)")
	flags.StringVar(&opts.alias, "alias", "", "one S3 alias from OC config.json (required in local mode)")
	flags.StringVar(&opts.dataDir, "data-dir", "", "writable application data directory (default CONFIG_DIR/console-data)")
	flags.BoolVar(&opts.containerListen, "container-listen", false, "allow wildcard binding inside a container; requires --public-url")
	flags.StringVar(&opts.publicURL, "public-url", "", "browser origin: local HTTP loopback or native HTTPS")
	flags.StringVar(&opts.address, "address", "127.0.0.1:9090", "literal IP and port (local mode requires loopback)")
	flags.StringVar(&opts.s3CA, "s3-ca", "", "S3 PEM CA file, replacing certs/CAs custom trust")
	flags.StringVar(&opts.adminURL, "admin-url", "", "independent OtterIO management root URL")
	flags.StringVar(&opts.adminCA, "admin-ca", "", "independent management PEM CA file")
	flags.BoolVar(&opts.allowWrites, "allow-writes", false, "enable explicitly confirmed object writes, bucket management/settings and IAM changes")
	flags.BoolVar(&opts.allowSharing, "allow-sharing", false, "enable explicitly requested time-limited download links")
	flags.StringVar(&opts.shareURL, "share-url", "", "S3 root URL reachable by download-link recipients")
	flags.StringVar(&opts.archiveDir, "archive-dir", "", "temporary directory for complete ZIP downloads (default system temporary directory)")
	flags.Int64Var(&opts.maxArchiveSize, "max-archive-size", 5<<30, "maximum source bytes in one archive (1 to 5368709120)")
	flags.Int64Var(&opts.maxUploadSize, "max-upload-size", 1<<30, "maximum file size in bytes (1 to 5368709120)")
	showVersion := flags.Bool("version", false, "print version and exit")
	flags.Usage = func() {
		fmt.Fprintln(errorOutput, "Usage: oc-console --alias NAME [options]\n       oc-console --auth-mode native --s3-url URL --public-url URL --tls-cert FILE --tls-key FILE [options]\n\nA storage console, read-only by default. Native mode requires HTTPS and independent IAM credentials.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid command options; see --help")
	}
	if *showVersion {
		_, err := fmt.Fprintln(output, "oc-console", version)
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments; see --help")
	}
	if opts.maxUploadSize < 1 || opts.maxUploadSize > 5<<30 {
		return errors.New("--max-upload-size must be between 1 and 5368709120 bytes")
	}
	if opts.maxArchiveSize < 1 || opts.maxArchiveSize > 5<<30 {
		return errors.New("--max-archive-size must be between 1 and 5368709120 bytes")
	}
	if opts.allowSharing && opts.shareURL == "" && os.Getenv("OC_SHARE_URL") == "" && os.Getenv("OC_SHARE_URL_"+opts.alias) == "" {
		return errors.New("--allow-sharing requires --share-url or OC_SHARE_URL for recipients")
	}
	if opts.authMode == "native" {
		return runNative(ctx, opts, output)
	}
	if opts.authMode != "local" {
		return errors.New("--auth-mode must be local or native")
	}
	if opts.s3URL != "" || opts.tlsCert != "" || opts.tlsKey != "" {
		return errors.New("--s3-url and browser TLS options require --auth-mode native")
	}
	if opts.containerListen {
		if err := validateContainerListenAddress(opts.address); err != nil {
			return err
		}
		if opts.publicURL == "" {
			return errors.New("--container-listen requires --public-url")
		}
	} else {
		if opts.publicURL != "" {
			return errors.New("--public-url requires --container-listen")
		}
		if err := validateListenAddress(opts.address); err != nil {
			return err
		}
	}
	if opts.configDir == "" {
		var err error
		opts.configDir, err = defaultConfigDir(os.Getenv)
		if err != nil {
			return err
		}
	}
	clientConfig, err := loadClientConfig(opts, os.Getenv)
	if err != nil {
		return err
	}
	backend, err := storageclient.New(clientConfig)
	if err != nil {
		return errors.New("invalid S3 or management alias configuration; check endpoints, API and addressing settings")
	}
	defer backend.Close()
	listener, err := net.Listen("tcp", opts.address)
	if err != nil {
		return errors.New("cannot listen on the requested address")
	}
	defer listener.Close()
	codeBytes := make([]byte, 24)
	if _, err := rand.Read(codeBytes); err != nil {
		return errors.New("cannot generate a console login code")
	}
	baseURL := "http://" + listener.Addr().String()
	if opts.containerListen {
		baseURL = opts.publicURL
	}
	loginCode := base64.RawURLEncoding.EncodeToString(codeBytes)
	if opts.dataDir == "" {
		opts.dataDir = filepath.Join(opts.configDir, "console-data")
	}
	identity := fmt.Sprintf("%x", sha256.Sum256([]byte(clientConfig.S3URL+"\x00"+clientConfig.AccessKey)))
	handler, err := console.New(console.Config{
		DataDir: opts.dataDir, Identity: identity,
		Backend: backend, Alias: opts.alias, BaseURL: baseURL,
		BackendFactory: func(ctx context.Context) (consoleapi.Backend, func(), error) {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			client, err := storageclient.New(clientConfig)
			if err != nil {
				return nil, nil, err
			}
			return client, func() { client.Close() }, nil
		},
		LoginCode: loginCode, SessionTTL: 30 * time.Minute,
		AllowWrites: opts.allowWrites, MaxUploadSize: opts.maxUploadSize,
		AllowSharing: opts.allowSharing, MaxArchiveSize: opts.maxArchiveSize, ArchiveDir: opts.archiveDir,
	})
	if err != nil {
		return errors.New("cannot initialize the console; check the browser URL and writable application data directory")
	}
	defer handler.Close()
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10,
	}
	mode := "read-only"
	if opts.allowWrites {
		mode = "writes enabled"
	}
	if _, err := fmt.Fprintf(output, "OC console (%s)\nURL: %s\nAlias: %s\nLogin code: %s\nSessions expire after 30 minutes. Keep this code private.\n", mode, baseURL, opts.alias, loginCode); err != nil {
		return errors.New("cannot print console startup information")
	}
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	var listenerError error
	select {
	case err := <-serveError:
		if !errors.Is(err, http.ErrServerClosed) {
			listenerError = errors.New("console listener stopped unexpectedly")
		}
	case <-ctx.Done():
	case <-handler.Done():
		_, _ = fmt.Fprintln(output, "The selected identity changed or an IAM outcome is uncertain. Verify its permissions and credentials in your terminal, update the alias if needed, and restart OC console.")
	}
	// Every exit path cancels sessions before draining HTTP and waits for owned
	// multipart cleanup before closing transports, including a failed Shutdown.
	_ = handler.Close()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownError := server.Shutdown(shutdownCtx)
	cancelShutdown()
	if shutdownError != nil {
		_ = server.Close()
		shutdownError = errors.New("console HTTP shutdown exceeded five seconds")
	}
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
	cleanupError := handler.Wait(cleanupCtx)
	cancelCleanup()
	if cleanupError != nil {
		cleanupError = errors.New("console tasks did not stop within their five-second cleanup grace period")
	}
	return errors.Join(listenerError, shutdownError, cleanupError)
}
