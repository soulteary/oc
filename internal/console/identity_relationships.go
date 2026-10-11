// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import "time"

// A relation is authoritative only for its bound target, revision and lifetime.
// Unknown and stale snapshots must never be interpreted as an empty impact set.
type identityRelationshipState uint8

const (
	identityRelationshipUnknown identityRelationshipState = iota
	identityRelationshipKnown
	identityRelationshipExpired
)

type identityRelationshipSnapshot struct {
	target   string
	revision string
	expires  time.Time
	scopes   []identityScope
	state    identityRelationshipState
}

func (snapshot identityRelationshipSnapshot) impact(target, revision string, now time.Time) ([]identityScope, error) {
	if snapshot.state != identityRelationshipKnown || target == "" || snapshot.target != target || revision == "" || snapshot.revision != revision || snapshot.expires.IsZero() || !now.Before(snapshot.expires) {
		return nil, errUnknownIdentityScope
	}
	result := make([]identityScope, 0, len(snapshot.scopes))
	seen := make(map[identityScope]bool)
	for _, scope := range snapshot.scopes {
		if !scope.valid() {
			return nil, errUnknownIdentityScope
		}
		if !seen[scope] {
			result = append(result, scope)
			seen[scope] = true
		}
	}
	return result, nil
}
