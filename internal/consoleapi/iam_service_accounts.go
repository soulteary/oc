// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package consoleapi

import "context"

// The service account condition protects its credential, restriction and native
// parent in the same graph revision. This interface never edits parent policies.
type IAMRelationshipServiceAccountsBackend interface {
	IAMMutateRelationshipServiceAccount(context.Context, IAMActionRequest, IAMRelationships) (IAMServiceAccountMutationResult, error)
}
type IAMServiceAccountMutationResult struct {
	Outcome        string            `json:"outcome"`
	ServiceAccount IAMServiceAccount `json:"serviceAccount"`
	Impact         IAMRelationships  `json:"impact"`
}
