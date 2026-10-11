// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

// iamUserMutationPlan captures parent and child scope before any write. No response can replace
// this trusted retirement set, including a lost acknowledgement or expired proof.
type iamUserPlanUse struct {
	mu   sync.Mutex
	used bool
}
type iamUserMutationPlan struct {
	lifetime                 *backendLifetime
	actorIdentity, actorUser string
	owner                    *session
	backend                  consoleapi.IAMRelationshipUsersBackend
	owned                    consoleapi.Backend
	use                      *iamUserPlanUse
	args                     consoleapi.IAMActionRequest
	relation                 consoleapi.IAMRelationships
	scopes                   []identityScope
	expires                  time.Time
}

func validPreparedUserAction(args consoleapi.IAMActionRequest) bool {
	switch args.Action {
	case "user.enable", "user.disable", "user.delete":
		return consoleapi.ValidIAMAction(args)
	}
	return false
}
func prepareIAMUserMutation(ctx context.Context, sess *session, args consoleapi.IAMActionRequest) (iamUserMutationPlan, error) {
	var plan iamUserMutationPlan
	if sess == nil || sess.oidc != nil || !consoleapi.ValidIAMAccessKey(sess.nativeUser) || sess.ctx == nil || sess.ctx.Err() != nil || !validPreparedUserAction(args) {
		return plan, errUnknownIdentityScope
	}
	backend, ok := sess.runtime.backend.(consoleapi.IAMRelationshipsBackend)
	if !ok {
		return plan, errUnknownIdentityScope
	}
	mutationBackend, ok := sess.runtime.backend.(consoleapi.IAMRelationshipUsersBackend)
	if !ok {
		return plan, errUnknownIdentityScope
	}
	targetBackend, ok := backend.(interface{ IAMStorageTarget() string })
	if !ok || targetBackend.IAMStorageTarget() == "" {
		return plan, errUnknownIdentityScope
	}
	ownedTarget := targetBackend.IAMStorageTarget()
	relation, err := backend.IAMRelationships(ctx, "user", args.User)
	if err != nil {
		return plan, err
	}
	if relation.Kind != "user" || relation.Target != args.User || relation.StorageTarget != ownedTarget || !relation.CoupledUsers {
		return plan, errUnknownIdentityScope
	}
	scopes, err := relationshipImpactFromSnapshot(relation, ownedTarget, time.Now())
	if err != nil {
		return plan, err
	}
	found := false
	for _, scope := range scopes {
		if scope == nativeUserScope(args.User) {
			found = true
		}
	}
	if !found {
		return plan, errUnknownIdentityScope
	}
	expires := relation.ExpiresAt
	relation.AffectedAccessKeys = append([]string(nil), relation.AffectedAccessKeys...)
	return iamUserMutationPlan{owner: sess, lifetime: sess.runtime.lifetime, actorIdentity: sess.runtime.identity, actorUser: sess.nativeUser, backend: mutationBackend, owned: sess.runtime.backend, use: &iamUserPlanUse{}, args: args, relation: relation, scopes: append([]identityScope{}, scopes...), expires: expires}, nil
}
func (plan iamUserMutationPlan) responseMatches(result consoleapi.IAMUserMutationResult) bool {
	expectedStatus := map[string]string{"user.enable": "enabled", "user.disable": "disabled", "user.delete": "deleted"}[plan.args.Action]
	if result.Outcome != "confirmed" || result.User.AccessKey != plan.args.User || result.User.Status != expectedStatus || result.Impact.Kind != "user" || result.Impact.Target != plan.args.User || result.Impact.StorageTarget != plan.relation.StorageTarget || !result.Impact.CoupledUsers || result.Impact.State != "known" || result.Impact.AffectedAccessKeys == nil || len(result.Impact.AffectedAccessKeys) != len(plan.scopes) {
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

// executeIAMUserMutation is intentionally called only by the dispatcher, which may
// call it only after native actor/write/CSRF checks and protocol capability gating.
func (s *Server) executeIAMUserMutation(w http.ResponseWriter, r *http.Request, sess *session, plan iamUserMutationPlan) {
	if plan.use == nil || plan.owner != sess || sess == nil || sess.oidc != nil || sess.ctx == nil || sess.ctx.Err() != nil || !consoleapi.ValidIAMAccessKey(sess.nativeUser) || sess.nativeUser != plan.actorUser || sess.runtime.identity != plan.actorIdentity || sess.runtime.lifetime != plan.lifetime || !sameOwnedIAMUserBackend(sess.runtime.backend, plan.owned) || plan.backend == nil {
		writeError(w, 409, "identity_plan_invalid", "Prepare a new user change in this session.")
		return
	}
	targetBackend, ok := plan.owned.(interface{ IAMStorageTarget() string })
	if !ok || targetBackend.IAMStorageTarget() != plan.relation.StorageTarget {
		writeError(w, 409, "identity_plan_invalid", "Prepare a new user change in this session.")
		return
	}
	plan.use.mu.Lock()
	if plan.use.used {
		plan.use.mu.Unlock()
		writeError(w, 409, "identity_plan_consumed", "Prepare a new user change.")
		return
	}
	plan.use.used = true
	plan.use.mu.Unlock()
	if plan.expires.IsZero() || !time.Now().Before(plan.expires) {
		writeError(w, 409, "identity_impact_expired", "Reload user relationships before changing this user.")
		return
	}
	mutation := s.beginIdentityMutation(w, sess, identityImpact{known: true, scopes: append([]identityScope{}, plan.scopes...)})
	if mutation == nil {
		return
	}
	revoke := false
	finish := func() { mutation.finish(revoke) }
	defer finish()
	result, err := plan.backend.IAMMutateRelationshipUser(r.Context(), plan.args, plan.relation)
	if err != nil {
		var failure *consoleapi.Error
		if errors.As(err, &failure) && failure != nil && failure.Code != "outcome_unknown" && ((failure.Status >= 400 && failure.Status < 500) || failure.Status == 501) {
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
		}{"outcome_unknown", "The user may have changed. Verify its state before another change; affected sessions require a fresh login.", loginRequired})
		return
	}
	writeJSON(w, 200, struct {
		consoleapi.IAMUserMutationResult
		LoginRequired bool `json:"loginRequired"`
	}{result, loginRequired})
}

func sameOwnedIAMUserBackend(a, b consoleapi.Backend) bool {
	kind := reflect.TypeOf(a)
	return kind != nil && kind.Comparable() && a == b
}
