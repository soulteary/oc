// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type nativeRelationshipBindingBackend interface {
	consoleapi.IAMRelationshipsBackend
	consoleapi.IAMRelationshipMutationsBackend
	IAMStorageTarget() string
	IAMBindings(context.Context, string, string) (consoleapi.IAMBindings, error)
}

type nativeRelationshipBindingPlan struct {
	mu       sync.Mutex
	used     bool
	owner    *session
	backend  nativeRelationshipBindingBackend
	binding  consoleapi.IAMBindings
	relation consoleapi.IAMRelationships
	impact   identityImpact
	policies []string
}

func prepareNativeRelationshipBindingPlan(ctx context.Context, sess *session, args consoleapi.IAMActionRequest) (*nativeRelationshipBindingPlan, error) {
	if sess == nil || !consoleapi.ValidIAMAccessKey(sess.nativeUser) || !consoleapi.ValidIAMAction(args) || (args.Action != "user.policies" && args.Action != "group.policies") {
		return nil, errUnknownIdentityScope
	}
	backend, ok := sess.runtime.backend.(nativeRelationshipBindingBackend)
	if !ok {
		return nil, errUnknownIdentityScope
	}
	kind, target := "user", args.User
	if args.Action == "group.policies" {
		kind, target = "group", args.Group
	}
	namespace := backend.IAMStorageTarget()
	if namespace == "" {
		return nil, errUnknownIdentityScope
	}
	relation, err := backend.IAMRelationships(ctx, kind, target)
	if err != nil {
		return nil, err
	}
	if !relation.CoupledBindings || relation.Kind != kind || relation.Target != target {
		return nil, errUnknownIdentityScope
	}
	scopes, err := relationshipImpactFromSnapshot(relation, namespace, time.Now())
	if err != nil {
		return nil, err
	}
	if kind == "user" {
		included := false
		for _, scope := range scopes {
			if scope == nativeUserScope(target) {
				included = true
			}
		}
		if !included {
			return nil, errUnknownIdentityScope
		}
	}
	binding, err := backend.IAMBindings(ctx, kind, target)
	if err != nil {
		return nil, err
	}
	if !binding.Conditional || binding.Kind != kind || binding.Target != target || binding.Policies == nil {
		return nil, errUnknownIdentityScope
	}
	// Keep the browser's reviewed revision. A fresh read must not silently turn
	// an obsolete review into permission to overwrite a concurrent policy edit.
	if binding.Revision != args.Revision {
		return nil, &consoleapi.Error{Status: 409, Code: "identity_revision_conflict", Message: "Policy bindings changed. Read them again before reviewing another change."}
	}
	relation.AffectedAccessKeys = append([]string{}, relation.AffectedAccessKeys...)
	binding.Policies = append([]string{}, binding.Policies...)
	return &nativeRelationshipBindingPlan{owner: sess, backend: backend, binding: binding, relation: relation, impact: identityImpact{known: true, scopes: append([]identityScope{}, scopes...)}, policies: append([]string{}, args.Policies...)}, nil
}

func (plan *nativeRelationshipBindingPlan) consume(sess *session) bool {
	if plan == nil {
		return false
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	if plan.used || plan.owner != sess || sess == nil || !consoleapi.ValidIAMAccessKey(sess.nativeUser) || plan.backend.IAMStorageTarget() != plan.relation.StorageTarget || !time.Now().Before(plan.relation.ExpiresAt) {
		return false
	}
	plan.used = true
	return true
}

func (plan *nativeRelationshipBindingPlan) acknowledgementMatches(result consoleapi.IAMRelationshipMutationResult) bool {
	if result.Outcome != "confirmed" || result.Impact.State != "known" || result.Impact.Kind != plan.relation.Kind || result.Impact.Target != plan.relation.Target || result.Impact.StorageTarget != plan.relation.StorageTarget || result.Impact.AffectedAccessKeys == nil || len(result.Impact.AffectedAccessKeys) != len(plan.relation.AffectedAccessKeys) {
		return false
	}
	seen := map[string]bool{}
	for _, key := range plan.relation.AffectedAccessKeys {
		seen[key] = true
	}
	for _, key := range result.Impact.AffectedAccessKeys {
		if !seen[key] {
			return false
		}
		delete(seen, key)
	}
	return len(seen) == 0
}

func (s *Server) executeNativeRelationshipBindingPlan(w http.ResponseWriter, r *http.Request, sess *session, plan *nativeRelationshipBindingPlan) {
	if !plan.consume(sess) {
		writeError(w, 409, "identity_plan_expired", "Read the identity relationships again before a change.")
		return
	}
	mutation := s.beginIdentityMutation(w, sess, plan.impact)
	if mutation == nil {
		return
	}
	revoke := false
	defer func() { mutation.finish(revoke) }()
	result, err := plan.backend.IAMReplaceRelationshipBindings(r.Context(), plan.binding, plan.relation, plan.policies)
	if err != nil {
		var failure *consoleapi.Error
		if errors.As(err, &failure) && failure != nil && failure.Code != "outcome_unknown" && (failure.Status >= 400 && failure.Status < 500 || failure.Status == 501) {
			mutation.finish(false)
			writeBackendError(w, failure)
			return
		}
	}
	revoke = true
	loginRequired := plan.impact.matches(sess)
	if !loginRequired {
		mutation.finish(true)
	}
	if err != nil || !plan.acknowledgementMatches(result) {
		writeJSON(w, 502, struct {
			Code          string `json:"code"`
			Message       string `json:"message"`
			LoginRequired bool   `json:"loginRequired"`
		}{"outcome_unknown", "The policy change may have completed. Verify the exact target before another change.", loginRequired})
		return
	}
	writeJSON(w, 200, struct {
		Outcome       string                 `json:"outcome"`
		Bindings      consoleapi.IAMBindings `json:"bindings"`
		LoginRequired bool                   `json:"loginRequired"`
	}{"confirmed", result.Bindings, loginRequired})
}
