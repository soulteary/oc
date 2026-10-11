// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package consoleapi

import "context"

// RangeBackend pins a closed byte interval to identity-authorized metadata.
// OpenRange must reject ignored ranges, changed ETags, mismatched versions and
// missing protected-version capability before exposing a response body.
// The returned Object.Size is the interval length, not the full object length.
type RangeBackend interface {
	ReferenceBackend
	OpenRange(context.Context, ObjectRef, ObjectInfo, int64, int64) (Object, error)
}
