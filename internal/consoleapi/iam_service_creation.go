// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package consoleapi

import "context"

// IAMRelationshipServiceCreationBackend refuses servers without locked native creation.
type IAMRelationshipServiceCreationBackend interface {
	IAMCreateRelationshipServiceAccount(context.Context, IAMActionRequest, IAMRelationships) (IAMActionResult, error)
}
