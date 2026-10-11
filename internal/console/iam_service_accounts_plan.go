// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
)

type serviceAccountPlanBackend interface {
	consoleapi.IAMRelationshipsBackend
	consoleapi.IAMRelationshipTargetBackend
	consoleapi.IAMRelationshipServiceAccountsBackend
}
type serviceAccountPlanUse struct {
	mu   sync.Mutex
	used bool
}
type nativeServiceAccountPlan struct {
	owner                    *session
	owned                    consoleapi.Backend
	lifetime                 *backendLifetime
	actorIdentity, actorUser string
	backend                  serviceAccountPlanBackend
	args                     consoleapi.IAMActionRequest
	relation                 consoleapi.IAMRelationships
	scopes                   []identityScope
	use                      *serviceAccountPlanUse
}

func validPreparedServiceAccountAction(args consoleapi.IAMActionRequest) bool {
	switch args.Action {
	case "service-account.enable", "service-account.disable", "service-account.rotate", "service-account.policy", "service-account.delete":
	default:
		return false
	}
	if !consoleapi.ValidIAMAction(args) || len(args.Policy) > 16<<10 {
		return false
	}
	if args.Action == "service-account.policy" {
		_, err := iampolicy.ParseConfig(strings.NewReader(args.Policy))
		return err == nil
	}
	return true
}
func prepareNativeServiceAccountPlan(ctx context.Context, sess *session, args consoleapi.IAMActionRequest) (*nativeServiceAccountPlan, error) {
	if sess == nil || sess.oidc != nil || !consoleapi.ValidIAMAccessKey(sess.nativeUser) || sess.ctx == nil || sess.ctx.Err() != nil || !validPreparedServiceAccountAction(args) {
		return nil, errUnknownIdentityScope
	}
	backend, ok := sess.runtime.backend.(serviceAccountPlanBackend)
	if !ok || backend.IAMStorageTarget() == "" {
		return nil, errUnknownIdentityScope
	}
	relation, err := backend.IAMRelationships(ctx, "service-account", args.AccessKey)
	if err != nil {
		return nil, err
	}
	if relation.Kind != "service-account" || relation.Target != args.AccessKey || !relation.CoupledServiceAccounts || !consoleapi.ValidIAMAccessKey(relation.NativeParent) || relation.NativeParent == args.AccessKey || len(relation.AffectedAccessKeys) != 1 || relation.AffectedAccessKeys[0] != args.AccessKey {
		return nil, errUnknownIdentityScope
	}
	scopes, err := relationshipImpactFromSnapshot(relation, backend.IAMStorageTarget(), time.Now())
	if err != nil {
		return nil, err
	}
	relation.AffectedAccessKeys = append([]string{}, relation.AffectedAccessKeys...)
	args.Members = append([]string(nil), args.Members...)
	args.Policies = append([]string(nil), args.Policies...)
	return &nativeServiceAccountPlan{owner: sess, owned: sess.runtime.backend, lifetime: sess.runtime.lifetime, actorIdentity: sess.runtime.identity, actorUser: sess.nativeUser, backend: backend, args: args, relation: relation, scopes: append([]identityScope{}, scopes...), use: &serviceAccountPlanUse{}}, nil
}
func (plan *nativeServiceAccountPlan) consume(sess *session, now time.Time) bool {
	if plan == nil || plan.use == nil {
		return false
	}
	plan.use.mu.Lock()
	defer plan.use.mu.Unlock()
	kind := reflect.TypeOf(plan.owned)
	if plan.use.used || sess == nil || plan.owner != sess || sess.oidc != nil || !consoleapi.ValidIAMAccessKey(sess.nativeUser) || plan.actorUser != sess.nativeUser || plan.actorIdentity != sess.runtime.identity || plan.lifetime != sess.runtime.lifetime || kind == nil || !kind.Comparable() || plan.owned != sess.runtime.backend || plan.backend.IAMStorageTarget() != plan.relation.StorageTarget || !now.Before(plan.relation.ExpiresAt) {
		return false
	}
	plan.use.used = true
	return true
}
func (plan *nativeServiceAccountPlan) responseMatches(result consoleapi.IAMServiceAccountMutationResult) bool {
	if result.Outcome != "confirmed" || result.ServiceAccount.AccessKey != plan.args.AccessKey || result.ServiceAccount.ParentUser != plan.relation.NativeParent || result.Impact.Kind != "service-account" || result.Impact.Target != plan.args.AccessKey || result.Impact.State != "known" || !result.Impact.CoupledServiceAccounts || result.Impact.NativeParent != plan.relation.NativeParent || result.Impact.StorageTarget != plan.relation.StorageTarget || len(result.Impact.AffectedAccessKeys) != 1 || result.Impact.AffectedAccessKeys[0] != plan.args.AccessKey {
		return false
	}
	if plan.args.Action == "service-account.policy" {
		wanted, err := iampolicy.ParseConfig(strings.NewReader(plan.args.Policy))
		actual, actualErr := iampolicy.ParseConfig(strings.NewReader(result.ServiceAccount.Policy))
		if err != nil || actualErr != nil || result.ServiceAccount.ImpliedPolicy || !reflect.DeepEqual(wanted, actual) {
			return false
		}
	}
	if plan.args.Action == "service-account.delete" {
		return result.ServiceAccount.Status == "deleted"
	}
	if result.ServiceAccount.Status != "on" && result.ServiceAccount.Status != "off" {
		return false
	}
	return (plan.args.Action != "service-account.enable" || result.ServiceAccount.Status == "on") && (plan.args.Action != "service-account.disable" || result.ServiceAccount.Status == "off")
}

// The shared dispatcher owns explicit native/write/CSRF authorization. This
// helper remains unregistered until the server atomic capability is verified.
func (s *Server) executeNativeServiceAccountPlan(w http.ResponseWriter, r *http.Request, sess *session, plan *nativeServiceAccountPlan) {
	if !plan.consume(sess, time.Now()) {
		writeError(w, 409, "identity_plan_invalid", "Prepare a new service account change in this session.")
		return
	}
	mutation := s.beginIdentityMutation(w, sess, identityImpact{known: true, scopes: append([]identityScope{}, plan.scopes...)})
	if mutation == nil {
		plan.args.SecretKey = ""
		return
	}
	revoke := false
	finish := func() { mutation.finish(revoke) }
	defer finish()
	args := plan.args
	plan.args.SecretKey = ""
	result, err := plan.backend.IAMMutateRelationshipServiceAccount(r.Context(), args, plan.relation)
	args.SecretKey = ""
	if err != nil {
		var failure *consoleapi.Error
		if errors.As(err, &failure) && failure != nil && failure.Code != "outcome_unknown" && ((failure.Status >= 400 && failure.Status < 500) || failure.Status == 501) {
			finish()
			writeBackendError(w, failure)
			return
		}
	}
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
		}{"outcome_unknown", "The service account may have changed. Verify its state before another change; its existing sessions require a fresh login.", loginRequired})
		return
	}
	writeJSON(w, 200, struct {
		consoleapi.IAMServiceAccountMutationResult
		LoginRequired bool `json:"loginRequired"`
	}{result, loginRequired})
}
