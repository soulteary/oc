// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package consoleapi

import (
	"context"
	"time"
)

// IAMRelationshipsBackend is optional. A known query snapshot does not grant
// permission to mutate: writes require a separate server-side atomic condition.
type IAMRelationshipsBackend interface {
	IAMRelationships(context.Context, string, string) (IAMRelationships, error)
}

type IAMRelationships struct {
	Kind               string    `json:"kind"`
	Target             string    `json:"target"`
	State              string    `json:"state"`
	Revision           string    `json:"revision"`
	ExpiresAt          time.Time `json:"expiresAt"`
	NativeParent       string    `json:"nativeParent,omitempty"`
	AffectedAccessKeys []string  `json:"affectedAccessKeys"`
	// StorageTarget is bound by the owned client, never accepted from the server
	// response or browser. It isolates identical access keys on different stores.
	StorageTarget string `json:"-"`
}
