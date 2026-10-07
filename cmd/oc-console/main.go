// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

// oc-console is an opt-in, single-operator console for one configured S3 alias.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/soulteary/mc/internal/console"
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
	flags.StringVar(&opts.configDir, "config-dir", "", "OC configuration directory (default OC_CONFIG_DIR, MC_CONFIG_DIR, or user profile)")
	flags.StringVar(&opts.alias, "alias", "", "one S3 alias from OC config.json (required)")
	flags.StringVar(&opts.address, "address", "127.0.0.1:9090", "literal loopback IP and port")
	flags.StringVar(&opts.s3CA, "s3-ca", "", "S3 PEM CA file, replacing certs/CAs custom trust")
	flags.StringVar(&opts.adminURL, "admin-url", "", "independent OtterIO management root URL")
	flags.StringVar(&opts.adminCA, "admin-ca", "", "independent management PEM CA file")
	flags.BoolVar(&opts.allowWrites, "allow-writes", false, "enable explicitly confirmed object uploads and deletes")
	flags.Int64Var(&opts.maxUploadSize, "max-upload-size", 1<<30, "maximum file size in bytes (1 to 5368709120)")
	showVersion := flags.Bool("version", false, "print version and exit")
	flags.Usage = func() {
		fmt.Fprintln(errorOutput, "Usage: oc-console --alias NAME [options]\n\nA local console, read-only by default. Credentials stay in the OC process.")
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
		return errors.New("unexpected positional arguments; use --alias NAME")
	}
	if opts.maxUploadSize < 1 || opts.maxUploadSize > 5<<30 {
		return errors.New("--max-upload-size must be between 1 and 5368709120 bytes")
	}
	if err := validateListenAddress(opts.address); err != nil {
		return err
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
		return errors.New("cannot listen on the requested loopback address")
	}
	defer listener.Close()
	codeBytes := make([]byte, 24)
	if _, err := rand.Read(codeBytes); err != nil {
		return errors.New("cannot generate a console login code")
	}
	baseURL := "http://" + listener.Addr().String()
	loginCode := base64.RawURLEncoding.EncodeToString(codeBytes)
	handler, err := console.New(console.Config{
		Backend: backend, Alias: opts.alias, BaseURL: baseURL,
		LoginCode: loginCode, SessionTTL: 30 * time.Minute,
		AllowWrites: opts.allowWrites, MaxUploadSize: opts.maxUploadSize,
	})
	if err != nil {
		return errors.New("cannot initialize the console")
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
