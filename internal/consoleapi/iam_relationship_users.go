// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package consoleapi

import "context"

// IAMRelationshipUsersBackend performs one graph-conditioned native user write.
type IAMRelationshipUsersBackend interface {
	IAMMutateRelationshipUser(context.Context, IAMActionRequest, IAMRelationships) (IAMUserMutationResult, error)
}
type IAMUserMutationState struct {
	AccessKey string `json:"accessKey"`
	Status    string `json:"status"`
}
type IAMUserMutationResult struct {
	Outcome string               `json:"outcome"`
	User    IAMUserMutationState `json:"user"`
	Impact  IAMRelationships     `json:"impact"`
}
