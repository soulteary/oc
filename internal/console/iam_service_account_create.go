// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"net/http"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type nativeServiceCreationBackend interface {
	consoleapi.IAMRelationshipsBackend
	consoleapi.IAMRelationshipServiceCreationBackend
	IAMStorageTarget() string
}

// Creation must not fall back to the legacy endpoint on an older server. The
// signed graph proof binds the reviewed parent, and the server checks current
// actor/parent authorization and access-key absence under its persistence lock.
func (s *Server) createNativeServiceAccount(w http.ResponseWriter, r *http.Request, sess *session, args consoleapi.IAMActionRequest) {
	if sess == nil || sess.oidc != nil || sess.ctx == nil || sess.ctx.Err() != nil || !consoleapi.ValidIAMAccessKey(sess.nativeUser) || args.Action != "service-account.create" || !consoleapi.ValidIAMAction(args) {
		writeError(w, 409, "identity_plan_invalid", "Prepare a new service account creation in this session.")
		return
	}
	backend, ok := sess.runtime.backend.(nativeServiceCreationBackend)
	if !ok || backend.IAMStorageTarget() == "" {
		s.writeNativeRelationshipPlanError(w, errUnknownIdentityScope)
		return
	}
	owned, lifetime, actorIdentity, actorUser := sess.runtime.backend, sess.runtime.lifetime, sess.runtime.identity, sess.nativeUser
	namespace := backend.IAMStorageTarget()
	relation, err := backend.IAMRelationships(r.Context(), "user", args.User)
	if err != nil {
		s.writeNativeRelationshipPlanError(w, err)
		return
	}
	if !relation.CoupledServiceCreation || relation.Kind != "user" || relation.Target != args.User || relation.StorageTarget != namespace {
		s.writeNativeRelationshipPlanError(w, errUnknownIdentityScope)
		return
	}
	scopes, err := relationshipImpactFromSnapshot(relation, namespace, time.Now())
	if err != nil {
		s.writeNativeRelationshipPlanError(w, err)
		return
	}
	found := false
	for _, scope := range scopes {
		if scope == nativeUserScope(args.User) {
			found = true
		}
	}
	if !found || !sameOwnedIAMUserBackend(sess.runtime.backend, owned) || sess.runtime.lifetime != lifetime || sess.runtime.identity != actorIdentity || sess.nativeUser != actorUser || sess.ctx == nil || sess.ctx.Err() != nil || backend.IAMStorageTarget() != namespace || !time.Now().Before(relation.ExpiresAt) {
		writeError(w, 409, "identity_plan_invalid", "Reload the parent before creating this service account.")
		return
	}
	relation.AffectedAccessKeys = append([]string(nil), relation.AffectedAccessKeys...)
	if !s.startSettingsWrite(w, sess, false) {
		return
	}
	defer s.finishSettingsWrite(false)
	generated := args.SecretKey == ""
	result, err := backend.IAMCreateRelationshipServiceAccount(r.Context(), args, relation)
	args.SecretKey = ""
	if err != nil {
		writeBackendError(w, err)
		return
	}
	validCredential := !generated && result.Credentials == nil
	if generated && result.Credentials != nil {
		validCredential = consoleapi.ValidIAMAccessKey(result.Credentials.AccessKey) && len(result.Credentials.SecretKey) >= 8 && (args.AccessKey == "" || args.AccessKey == result.Credentials.AccessKey)
	}
	if result.Outcome != "confirmed" || result.RestartRequired || !validCredential {
		writeError(w, 502, "outcome_unknown", "The service account may have been created. Verify the exact target before another request.")
		return
	}
	// Creating an absent child does not change any existing identity. A generated
	// credential is returned once by the verified single-dispatch client.
	writeJSON(w, 200, result)
}
