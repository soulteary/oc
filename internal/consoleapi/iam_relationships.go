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
	CoupledServiceAccounts bool      `json:"coupledServiceAccounts"`
	CoupledUserSecret      bool      `json:"coupledUserSecret"`
	CoupledServiceCreation bool      `json:"coupledServiceCreation"`
	CoupledUsers           bool      `json:"coupledUsers"`
	CoupledGroups          bool      `json:"coupledGroups"`
	CoupledBindings        bool      `json:"coupledBindings"`
	Kind                   string    `json:"kind"`
	Target                 string    `json:"target"`
	State                  string    `json:"state"`
	Revision               string    `json:"revision"`
	ExpiresAt              time.Time `json:"expiresAt"`
	NativeParent           string    `json:"nativeParent,omitempty"`
	AffectedAccessKeys     []string  `json:"affectedAccessKeys"`
	// StorageTarget is bound by the owned client, never accepted from the server
	// response or browser. It isolates identical access keys on different stores.
	StorageTarget string `json:"-"`
}

// IAMRelationshipMutationsBackend sends a single request with both the binding
// and relationship conditions. It never falls back to an unconditional write.
type IAMRelationshipMutationsBackend interface {
	IAMReplaceRelationshipBindings(context.Context, IAMBindings, IAMRelationships, []string) (IAMRelationshipMutationResult, error)
}
type IAMRelationshipMutationResult struct {
	Outcome  string           `json:"outcome"`
	Bindings IAMBindings      `json:"bindings"`
	Impact   IAMRelationships `json:"impact"`
}

// IAMRelationshipTargetBackend supplies the immutable storage namespace from
// the owned connection configuration, independently of a query response.
type IAMRelationshipTargetBackend interface{ IAMStorageTarget() string }
type IAMRelationshipSecretsBackend interface {
	IAMRotateRelationshipUserSecret(context.Context, IAMBindings, IAMRelationships, string) (IAMRelationshipMutationResult, error)
}
