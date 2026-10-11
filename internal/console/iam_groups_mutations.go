// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

// iamGroupMutationPlan captures scope before any write. No response can replace
// this trusted retirement set, including a lost acknowledgement or expired proof.
type iamGroupMutationPlan struct {
	args     consoleapi.IAMActionRequest
	relation consoleapi.IAMRelationships
	scopes   []identityScope
	expires  time.Time
}

func validPreparedGroupAction(args consoleapi.IAMActionRequest) bool {
	switch args.Action {
	case "group.add-members", "group.remove-members", "group.enable", "group.disable", "group.delete":
		return consoleapi.ValidIAMAction(args)
	}
	return false
}
func prepareIAMGroupMutation(ctx context.Context, backend consoleapi.IAMRelationshipsBackend, args consoleapi.IAMActionRequest) (iamGroupMutationPlan, error) {
	var plan iamGroupMutationPlan
	if backend == nil || !validPreparedGroupAction(args) {
		return plan, errUnknownIdentityScope
	}
	targetBackend, ok := backend.(interface{ IAMStorageTarget() string })
	if !ok || targetBackend.IAMStorageTarget() == "" {
		return plan, errUnknownIdentityScope
	}
	ownedTarget := targetBackend.IAMStorageTarget()
	// Always fetch group first. Its all-graph condition makes any later membership,
	// policy, child credential or account change conflict at the atomic write.
	relation, err := backend.IAMRelationships(ctx, "group", args.Group)
	if err != nil {
		return plan, err
	}
	if relation.Kind != "group" || relation.Target != args.Group || relation.StorageTarget != ownedTarget || !relation.CoupledGroups {
		return plan, errUnknownIdentityScope
	}
	scopes, err := relationshipImpactFromSnapshot(relation, ownedTarget, time.Now())
	if err != nil {
		return plan, err
	}
	expires := relation.ExpiresAt
	seen := map[identityScope]bool{}
	for _, scope := range scopes {
		seen[scope] = true
	}
	if args.Action == "group.add-members" {
		for _, member := range args.Members {
			user, err := backend.IAMRelationships(ctx, "user", member)
			if err != nil {
				return plan, err
			}
			if user.Kind != "user" || user.Target != member {
				return plan, errUnknownIdentityScope
			}
			added, err := relationshipImpactFromSnapshot(user, ownedTarget, time.Now())
			if err != nil {
				return plan, err
			}
			if user.ExpiresAt.Before(expires) {
				expires = user.ExpiresAt
			}
			memberPresent := false
			for _, scope := range added {
				if scope == nativeUserScope(member) {
					memberPresent = true
				}
				seen[scope] = true
			}
			if !memberPresent {
				return plan, errUnknownIdentityScope
			}
		}
	}
	if _, err := relationshipImpactFromSnapshot(relation, ownedTarget, time.Now()); err != nil {
		return plan, err
	}
	if !time.Now().Before(expires) {
		return plan, errUnknownIdentityScope
	}
	scopes = make([]identityScope, 0, len(seen))
	keys := make([]string, 0, len(seen))
	for scope := range seen {
		scopes = append(scopes, scope)
		keys = append(keys, scope.key)
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].key < scopes[j].key })
	sort.Strings(keys)
	args.Members = append([]string{}, args.Members...)
	args.Policies = append([]string(nil), args.Policies...)
	relation.AffectedAccessKeys = keys
	return iamGroupMutationPlan{args: args, relation: relation, scopes: append([]identityScope{}, scopes...), expires: expires}, nil
}
func (plan iamGroupMutationPlan) responseMatches(result consoleapi.IAMGroupMutationResult) bool {
	if result.Group.Members == nil || result.Group.Policies == nil {
		return false
	}
	if plan.args.Action == "group.delete" {
		if result.Group.Status != "deleted" || len(result.Group.Members) != 0 {
			return false
		}
	} else if result.Group.Status != "enabled" && result.Group.Status != "disabled" {
		return false
	}
	if plan.args.Action == "group.enable" && result.Group.Status != "enabled" || plan.args.Action == "group.disable" && result.Group.Status != "disabled" {
		return false
	}
	members := map[string]bool{}
	for _, member := range result.Group.Members {
		if !consoleapi.ValidIAMAccessKey(member) || members[member] {
			return false
		}
		members[member] = true
	}
	for _, member := range plan.args.Members {
		if plan.args.Action == "group.add-members" && !members[member] || plan.args.Action == "group.remove-members" && members[member] {
			return false
		}
	}
	if result.Outcome != "confirmed" || result.Group.Name != plan.args.Group || result.Impact.Kind != "group" || result.Impact.Target != plan.args.Group || result.Impact.StorageTarget != plan.relation.StorageTarget || !result.Impact.CoupledGroups || result.Impact.State != "known" || result.Impact.AffectedAccessKeys == nil || len(result.Impact.AffectedAccessKeys) != len(plan.scopes) {
		return false
	}
	expected := map[string]bool{}
	for _, scope := range plan.scopes {
		expected[scope.key] = true
	}
	for _, key := range result.Impact.AffectedAccessKeys {
		if !expected[key] {
			return false
		}
		delete(expected, key)
	}
	return len(expected) == 0
}

// executeIAMGroupMutation is intentionally unregistered. The dispatcher may
// call it only after native actor/write/CSRF checks and protocol capability gating.
func (s *Server) executeIAMGroupMutation(w http.ResponseWriter, r *http.Request, sess *session, backend consoleapi.IAMRelationshipGroupsBackend, plan iamGroupMutationPlan) {
	if plan.expires.IsZero() || !time.Now().Before(plan.expires) {
		writeError(w, 409, "identity_impact_expired", "Reload group relationships before changing this group.")
		return
	}
	mutation := s.beginIdentityMutation(w, sess, identityImpact{known: true, scopes: append([]identityScope{}, plan.scopes...)})
	if mutation == nil {
		return
	}
	revoke := false
	finish := func() { mutation.finish(revoke) }
	defer finish()
	result, err := backend.IAMMutateRelationshipGroup(r.Context(), plan.args, plan.relation)
	if err != nil {
		var failure *consoleapi.Error
		if errors.As(err, &failure) && failure.Code != "outcome_unknown" {
			finish()
			writeBackendError(w, failure)
			return
		}
	}
	// The write may have committed even if its acknowledgement is malformed,
	// stale or lost. Retirement uses the captured union, never post-write queries.
	revoke = true
	loginRequired := mutation.impact.matches(sess)
	if !loginRequired {
		finish()
	}
	if err != nil || !plan.responseMatches(result) {
		writeJSON(w, 502, struct {
			Code          string `json:"code"`
			Message       string `json:"message"`
			LoginRequired bool   `json:"loginRequired"`
		}{"outcome_unknown", "The group may have changed. Verify its state before another change; affected sessions require a fresh login.", loginRequired})
		return
	}
	writeJSON(w, 200, struct {
		consoleapi.IAMGroupMutationResult
		LoginRequired bool `json:"loginRequired"`
	}{result, loginRequired})
}
