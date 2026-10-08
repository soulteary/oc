// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/soulteary/mc/internal/clienttransport"
	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
)

const lifecycleTargetProtocolHeader = "X-Otterio-Lifecycle-Transition"

// requireLifecycleTargetProtocol keeps labeled targets from mutating a server
// whose older target implementation replaces every destination of the same type.
func requireLifecycleTargetProtocol(ctx context.Context, aliasedURL, bucket string) error {
	alias, _, cfg, err := expandAlias(aliasedURL)
	if err != nil {
		return fmt.Errorf("cannot resolve lifecycle target connection")
	}
	if cfg == nil {
		return fmt.Errorf("lifecycle target requires a configured host alias")
	}
	endpoint, caFile := resolveAdminSettings(alias, cfg)
	config := NewS3Config(endpoint, cfg)
	config.AdminCAFile = caFile
	return checkLifecycleTargetProtocol(ctx, config, bucket)
}

func checkLifecycleTargetProtocol(ctx context.Context, config *Config, bucket string) error {
	endpoint, err := clienttransport.ValidateAdminEndpoint(config.HostURL)
	if err != nil {
		return err
	}
	roots := globalRootCAs
	if config.AdminCAFile != "" {
		roots, _, err = clienttransport.LoadCAFile(config.AdminCAFile)
		if err != nil {
			return err
		}
	}
	transport := clienttransport.New(roots)
	transport.TLSClientConfig.InsecureSkipVerify = config.Insecure
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	endpoint.Path = "/otterio/admin/v3/list-remote-targets"
	endpoint.RawQuery = url.Values{"bucket": {bucket}, "type": {"ilm"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	req = signer.SignV4(*req, config.AccessKey, config.SecretKey, config.SessionToken, "")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("check lifecycle target support: %w", err)
	}
	defer resp.Body.Close()
	// Finish the bounded read before trusting a capability response.
	body, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return fmt.Errorf("cannot read lifecycle target capability response")
	}
	values := resp.Header.Values(lifecycleTargetProtocolHeader)
	if (resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound) || len(values) != 1 || values[0] != "v1" {
		return fmt.Errorf("server does not confirm lifecycle transition v1 support; install the matching console-server-p3.patch before managing ilm targets")
	}
	return nil
}
