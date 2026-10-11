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

type nativeUserSecretBackend interface {
	consoleapi.IAMRelationshipsBackend
	consoleapi.IAMRelationshipTargetBackend
	consoleapi.IAMRelationshipSecretsBackend
	IAMBindings(context.Context, string, string) (consoleapi.IAMBindings, error)
}

// The plan lives only on the server. Its owner, fixed storage namespace and
// immutable pre-submit scopes are never deserialized from a browser request.
type nativeUserSecretPlan struct {
	mu            sync.Mutex
	used          bool
	owner         *session
	lifetime      *backendLifetime
	actorIdentity string
	actorUser     string
	backend       nativeUserSecretBackend
	storageTarget string
	target        string
	binding       consoleapi.IAMBindings
	relation      consoleapi.IAMRelationships
	impact        identityImpact
}

func prepareNativeUserSecretPlan(ctx context.Context, sess *session, target string) (*nativeUserSecretPlan, error) {
	if sess == nil || !consoleapi.ValidIAMAccessKey(sess.nativeUser) || !consoleapi.ValidIAMAccessKey(target) {
		return nil, errUnknownIdentityScope
	}
	backend, ok := sess.runtime.backend.(nativeUserSecretBackend)
	if !ok {
		return nil, errUnknownIdentityScope
	}
	storageTarget := backend.IAMStorageTarget()
	if storageTarget == "" {
		return nil, errUnknownIdentityScope
	}
	relation, err := backend.IAMRelationships(ctx, "user", target)
	if err != nil {
		return nil, err
	}
	if !relation.CoupledUserSecret || relation.Kind != "user" || relation.Target != target {
		return nil, errUnknownIdentityScope
	}
	scopes, err := relationshipImpactFromSnapshot(relation, storageTarget, time.Now())
	if err != nil {
		return nil, err
	}
	if len(scopes) != len(relation.AffectedAccessKeys) {
		return nil, errUnknownIdentityScope
	}
	targetIncluded := false
	for _, scope := range scopes {
		if scope == nativeUserScope(target) {
			targetIncluded = true
		}
	}
	if !targetIncluded {
		return nil, errUnknownIdentityScope
	}
	binding, err := backend.IAMBindings(ctx, "user", target)
	if err != nil {
		return nil, err
	}
	if binding.Kind != "user" || binding.Target != target || !binding.Conditional || len(binding.Revision) != 64 || binding.Policies == nil {
		return nil, errUnknownIdentityScope
	}
	relation.AffectedAccessKeys = append([]string{}, relation.AffectedAccessKeys...)
	binding.Policies = append([]string{}, binding.Policies...)
	return &nativeUserSecretPlan{owner: sess, lifetime: sess.runtime.lifetime, actorIdentity: sess.runtime.identity, actorUser: sess.nativeUser, backend: backend, storageTarget: storageTarget, target: target, binding: binding, relation: relation, impact: identityImpact{known: true, scopes: append([]identityScope(nil), scopes...)}}, nil
}
func (plan *nativeUserSecretPlan) consume(sess *session, now time.Time) bool {
	if plan == nil {
		return false
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	if plan.used || plan.owner != sess || sess == nil || plan.lifetime != sess.runtime.lifetime || plan.actorIdentity != sess.runtime.identity || plan.actorUser != sess.nativeUser || !consoleapi.ValidIAMAccessKey(sess.nativeUser) || plan.backend.IAMStorageTarget() != plan.storageTarget || !now.Before(plan.relation.ExpiresAt) {
		return false
	}
	plan.used = true
	return true
}
func (plan *nativeUserSecretPlan) acknowledgementMatches(result consoleapi.IAMRelationshipMutationResult) bool {
	if result.Outcome != "confirmed" || result.Impact.State != "known" || result.Impact.Kind != "user" || result.Impact.Target != plan.target || result.Impact.StorageTarget != plan.storageTarget || result.Impact.AffectedAccessKeys == nil || len(result.Impact.AffectedAccessKeys) != len(plan.relation.AffectedAccessKeys) {
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

// Integration calls prepare from the authenticated owned backend, then execute
// with that exact server plan. Only the newly requested secret comes from UI.
func (s *Server) executeNativeUserSecretPlan(w http.ResponseWriter, r *http.Request, sess *session, plan *nativeUserSecretPlan, secret string) {
	target := ""
	if plan != nil {
		target = plan.target
	}
	if !consoleapi.ValidIAMAction(consoleapi.IAMActionRequest{Action: "user.rotate", User: target, SecretKey: secret, ConfirmTarget: target}) {
		writeError(w, 400, "invalid_request", "The new secret is invalid.")
		return
	}
	if !plan.consume(sess, time.Now()) {
		writeError(w, 409, "identity_plan_expired", "Prepare a fresh identity change for this session.")
		return
	}
	mutation := s.beginIdentityMutation(w, sess, plan.impact)
	if mutation == nil {
		return
	}
	revoke := false
	defer func() { mutation.finish(revoke) }()
	result, err := plan.backend.IAMRotateRelationshipUserSecret(r.Context(), plan.binding, plan.relation, secret)
	secret = ""
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
		}{"outcome_unknown", "The secret may have changed. Verify the current credentials before another change.", loginRequired})
		return
	}
	writeJSON(w, 200, struct {
		Outcome       string `json:"outcome"`
		LoginRequired bool   `json:"loginRequired"`
	}{"confirmed", loginRequired})
}
