// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package consoleapi

import "context"

// ServerDiagnostics is an administrative snapshot, not account-scoped usage.
// Node order is upstream order, without endpoints, paths, configuration or IDs.
type ServerDiagnostics struct {
	Nodes []DiagnosticNode `json:"nodes"`
}

type DiagnosticNode struct {
	State         string `json:"state"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	HeapBytes     uint64 `json:"heapBytes"`
	Drives        int    `json:"drives"`
}

// DiagnosticsBackend requires upstream admin:ServerInfo. Implementations must
// use the requesting session's identity and never substitute administrator keys.
type DiagnosticsBackend interface {
	ServerDiagnostics(context.Context) (ServerDiagnostics, error)
}
