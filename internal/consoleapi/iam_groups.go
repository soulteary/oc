// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package consoleapi

import "context"

// IAMRelationshipGroupsBackend never falls back to an unconditional group write.
type IAMRelationshipGroupsBackend interface {
	IAMMutateRelationshipGroup(context.Context, IAMActionRequest, IAMRelationships) (IAMGroupMutationResult, error)
}
type IAMGroupMutationResult struct {
	Outcome string           `json:"outcome"`
	Group   IAMGroup         `json:"group"`
	Impact  IAMRelationships `json:"impact"`
}
