// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/soulteary/mc/internal/clienttransport"
	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
)

const maxDiagnosticsReply = 1 << 20

var _ consoleapi.DiagnosticsBackend = (*Client)(nil)

// ServerDiagnostics reads one bounded snapshot. It neither retries nor exposes
// the full admin response; denied users never fall back to another identity.
func (c *Client) ServerDiagnostics(ctx context.Context) (consoleapi.ServerDiagnostics, error) {
	result := consoleapi.ServerDiagnostics{Nodes: []consoleapi.DiagnosticNode{}}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	credential, err := c.selectedCredentials(ctx)
	if err != nil {
		return result, err
	}
	if credential.SignerType != credentials.SignatureV4 {
		return result, &consoleapi.Error{Status: 501, Code: "diagnostics_unsupported", Message: "Diagnostics require signature V4."}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.adminEndpoint+"/otterio/admin/v3/info", nil)
	if err != nil {
		return result, configError()
	}
	setPayloadHash(req, nil)
	req = signer.SignV4(*req, credential.AccessKeyID, credential.SecretAccessKey, credential.SessionToken, "")
	resp, err := c.adminTransport.RoundTrip(clienttransport.NormalizeAdminRequest(req))
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return result, normalizeError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return result, &consoleapi.Error{Status: 403, Code: "diagnostics_denied", Message: "This identity cannot read server diagnostics."}
	}
	if resp.StatusCode != http.StatusOK {
		return result, diagnosticsUnavailable()
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDiagnosticsReply+1))
	if err != nil {
		return result, normalizeError(err)
	}
	if len(data) > maxDiagnosticsReply {
		return result, diagnosticsUnavailable()
	}
	// Decode only the whitelist. In particular no upstream arbitrary maps or
	// SDK InfoMessage can cross the browser boundary.
	var reply struct {
		Servers []*struct {
			State    *string                      `json:"state"`
			Version  json.RawMessage              `json:"version"`
			Uptime   json.RawMessage              `json:"uptime"`
			MemStats *struct{ HeapAlloc *uint64 } `json:"mem_stats"`
			Drives   json.RawMessage              `json:"drives"`
		} `json:"servers"`
	}
	if json.Unmarshal(data, &reply) != nil || len(reply.Servers) == 0 || len(reply.Servers) > 256 {
		return result, diagnosticsUnavailable()
	}
	for _, node := range reply.Servers {
		if node == nil || node.State == nil || node.MemStats == nil || node.MemStats.HeapAlloc == nil {
			return consoleapi.ServerDiagnostics{}, diagnosticsUnavailable()
		}
		if *node.State != "online" && *node.State != "offline" && *node.State != "initializing" {
			return consoleapi.ServerDiagnostics{}, diagnosticsUnavailable()
		}
		// The SDK omits zero uptime and empty drives. Offline peers additionally
		// omit version. MemStats and HeapAlloc are always serialized, even zero.
		var version *string
		if len(node.Version) != 0 && (json.Unmarshal(node.Version, &version) != nil || version == nil) {
			return consoleapi.ServerDiagnostics{}, diagnosticsUnavailable()
		}
		if *node.State != "offline" && (version == nil || *version == "") {
			return consoleapi.ServerDiagnostics{}, diagnosticsUnavailable()
		}
		var uptime *int64
		if len(node.Uptime) != 0 && (json.Unmarshal(node.Uptime, &uptime) != nil || uptime == nil || *uptime < 0) {
			return consoleapi.ServerDiagnostics{}, diagnosticsUnavailable()
		}
		var drives []*struct{}
		if len(node.Drives) != 0 {
			if json.Unmarshal(node.Drives, &drives) != nil || drives == nil {
				return consoleapi.ServerDiagnostics{}, diagnosticsUnavailable()
			}
			for _, drive := range drives {
				if drive == nil {
					return consoleapi.ServerDiagnostics{}, diagnosticsUnavailable()
				}
			}
		}
		entry := consoleapi.DiagnosticNode{State: *node.State, HeapBytes: *node.MemStats.HeapAlloc, Drives: len(drives)}
		if version != nil {
			if len(*version) > 256 {
				return consoleapi.ServerDiagnostics{}, diagnosticsUnavailable()
			}
			entry.Version = *version
		}
		if uptime != nil {
			entry.UptimeSeconds = *uptime
		}
		result.Nodes = append(result.Nodes, entry)
	}
	return result, nil
}

func diagnosticsUnavailable() *consoleapi.Error {
	return &consoleapi.Error{Status: 502, Code: "diagnostics_unavailable", Message: "Server diagnostics are unavailable."}
}
