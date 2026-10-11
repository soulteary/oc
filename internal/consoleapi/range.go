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
	// OpenRangeFull preserves the original current/version selector and pins all
	// metadata across a conditional or If-Range fallback GET.
	OpenRangeFull(context.Context, ObjectRef, ObjectInfo) (Object, error)
}

// FormatEntityTag formats a bare strong opaque tag using HTTP entity-tag syntax,
// which does not treat backslashes as escapes. Invalid header bytes are rejected.
func FormatEntityTag(opaque string) (string, bool) {
	if len(opaque) == 0 || len(opaque) > 512 {
		return "", false
	}
	for i := 0; i < len(opaque); i++ {
		c := opaque[i]
		if c == 0x22 || c < 0x21 || c == 0x7f {
			return "", false
		}
	}
	return "\"" + opaque + "\"", true
}
