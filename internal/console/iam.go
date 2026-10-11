// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

func (s *Server) isIAMRoute(path string) bool {
	return path == "/api/iam/users" || path == "/api/iam/groups" || path == "/api/iam/service-accounts" || path == "/api/iam/policies" || path == "/api/iam/actions" || path == "/api/iam/bindings"
}

func (s *Server) serveIAM(w http.ResponseWriter, r *http.Request) {
	method := http.MethodGet
	if r.URL.Path == "/api/iam/actions" {
		method = http.MethodPost
	}
	if !s.requireMethod(w, r, method) {
		return
	}
	sess, _ := s.authenticate(r)
	if sess == nil {
		writeError(w, 401, "login_required", s.loginPrompt())
		return
	}
	var nativeArgs *consoleapi.IAMActionRequest
	if method == http.MethodPost {
		if s.oidc != nil {
			writeError(w, 403, "identity_relationship_unknown", "OIDC identity has no authoritative native parent mapping; IAM changes are unavailable.")
			return
		}
		if !s.requireOrigin(w, r) || !s.requireCSRF(w, r, sess) {
			return
		}
		if s.nativeLogin != nil {
			nativeArgs = new(consoleapi.IAMActionRequest)
			if !decodeJSON(w, r, nativeArgs, 6*20*1024+8192) {
				return
			}
			if !nativeIAMActionAllowed(nativeArgs.Action) || (nativeArgs.Action == "group.create" && len(nativeArgs.Members) != 0) {
				writeError(w, 403, "identity_changes_disabled", "Native IAM supports creation and user status/deletion; other identity changes are unavailable.")
				return
			}
		}
		if sess.runtime.writer == nil {
			writeError(w, 403, "writes_disabled", "Restart OC with writes explicitly enabled to manage IAM.")
			return
		}
	}
	backend, ok := sess.runtime.backend.(consoleapi.IAMBackend)
	if !ok {
		writeError(w, 501, "iam_unsupported", "This connection does not support IAM management.")
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(r.URL.RawQuery) > maxQueryLength || (r.URL.Path != "/api/iam/service-accounts" && r.URL.Path != "/api/iam/bindings" && len(q) != 0) {
		writeError(w, 400, "invalid_input", "This IAM request contains invalid parameters.")
		return
	}
	if r.URL.Path == "/api/iam/service-accounts" && (len(q) != 1 || len(q["user"]) != 1 || !consoleapi.ValidIAMAccessKey(q.Get("user"))) {
		writeError(w, 400, "invalid_input", "Choose one exact parent user for the service accounts.")
		return
	}
	if r.URL.Path == "/api/iam/bindings" && (len(q) != 2 || len(q["kind"]) != 1 || len(q["target"]) != 1 || (q.Get("kind") != "user" && q.Get("kind") != "group") || !consoleapi.ValidIAMName(q.Get("target"))) {
		writeError(w, 400, "invalid_input", "Choose one exact native user or group to view direct policy bindings.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	stop := context.AfterFunc(sess.ctx, cancel)
	defer func() { stop(); cancel() }()
	if !acquire(w, ctx, s.apiSlots) {
		return
	}
	defer func() { <-s.apiSlots }()
	r = r.WithContext(ctx)
	var value any
	switch r.URL.Path {
	case "/api/iam/users":
		value, err = backend.IAMUsers(ctx)
	case "/api/iam/groups":
		value, err = backend.IAMGroups(ctx)
	case "/api/iam/service-accounts":
		value, err = backend.IAMServiceAccounts(ctx, q.Get("user"))
	case "/api/iam/policies":
		value, err = backend.IAMPolicies(ctx)
	case "/api/iam/bindings":
		value, err = backend.IAMBindings(ctx, q.Get("kind"), q.Get("target"))
	case "/api/iam/actions":
		s.iamAction(w, r, sess, backend, nativeArgs)
		return
	}
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, 200, value)
}

func (s *Server) iamAction(w http.ResponseWriter, r *http.Request, sess *session, backend consoleapi.IAMBackend, provided *consoleapi.IAMActionRequest) {
	var args consoleapi.IAMActionRequest
	if provided != nil {
		args = *provided
		provided.SecretKey = ""
	} else if !decodeJSON(w, r, &args, 6*20*1024+8192) {
		return
	}
	if strings.Contains(args.Action, "policies") && args.Action != "user.policies" && args.Action != "group.policies" {
		writeError(w, 501, "policy_bindings_unsupported", "Policy bindings remain read-only until the server supports conditional replacement.")
		return
	}
	if !consoleapi.ValidIAMAction(args) {
		writeError(w, 400, "invalid_input", "Choose a supported IAM action and confirm its exact target. Include only the fields required by that action.")
		return
	}
	if s.nativeLogin != nil && args.Action == "user.rotate" {
		plan, err := prepareNativeUserSecretPlan(r.Context(), sess, args.User)
		if err != nil {
			s.writeNativeRelationshipPlanError(w, err)
			return
		}
		s.executeNativeUserSecretPlan(w, r, sess, plan, args.SecretKey)
		args.SecretKey = ""
		return
	}
	if s.nativeLogin != nil && (args.Action == "user.policies" || args.Action == "group.policies") {
		plan, err := prepareNativeRelationshipBindingPlan(r.Context(), sess, args)
		if err != nil {
			s.writeNativeRelationshipPlanError(w, err)
			return
		}
		s.executeNativeRelationshipBindingPlan(w, r, sess, plan)
		return
	}
	if s.nativeLogin != nil && validPreparedServiceAccountAction(args) {
		plan, err := prepareNativeServiceAccountPlan(r.Context(), sess, args)
		if err != nil {
			s.writeNativeRelationshipPlanError(w, err)
			return
		}
		s.executeNativeServiceAccountPlan(w, r, sess, plan)
		args.SecretKey = ""
		return
	}
	if s.nativeLogin != nil && validPreparedGroupAction(args) {
		plan, err := prepareIAMGroupMutation(r.Context(), sess, args)
		if err != nil {
			s.writeNativeRelationshipPlanError(w, err)
			return
		}
		s.executeIAMGroupMutation(w, r, sess, plan)
		return
	}
	if s.nativeLogin != nil && (args.Action == "user.disable" || args.Action == "user.delete") {
		s.retireNativeUser(w, r, sess, backend, args)
		return
	}
	// Local IAM edits can invalidate the shared identity and reserve every
	// write slot. Native creation and account activation use one ordinary slot;
	// conditional creation must not replace existing identities.
	rotate := s.nativeLogin == nil
	if !s.startSettingsWrite(w, sess, rotate) {
		return
	}
	writePending := true
	finishWrite := func() {
		if writePending {
			s.finishSettingsWrite(rotate)
			writePending = false
		}
	}
	defer finishWrite()
	result, err := backend.IAMAction(r.Context(), args)
	args.SecretKey = ""
	if err != nil {
		var apiError *consoleapi.Error
		if errors.As(err, &apiError) && apiError != nil && apiError.Code != "outcome_unknown" {
			finishWrite()
			writeBackendError(w, apiError)
			return
		}
		if s.nativeLogin != nil {
			writeError(w, 502, "outcome_unknown", "The IAM operation may have completed. Verify the exact target before making another request.")
			return
		}
		// A lost IAM acknowledgement can leave the startup identity invalid.
		// Retire conservatively, even when the browser already disconnected.
		defer s.Close()
		writeJSON(w, 502, struct {
			Code            string `json:"code"`
			Message         string `json:"message"`
			RestartRequired bool   `json:"restartRequired"`
		}{"outcome_unknown", "The IAM change may have completed. Verify it with an administrative client, then restart OC before continuing.", true})
		return
	}
	if s.nativeLogin != nil && args.Action == "user.enable" && result.Outcome == "confirmed" {
		// Enabling an account does not change its credentials. The local SDK
		// conservatively marks self-target actions as requiring restart.
		result.RestartRequired = false
	}
	if s.nativeLogin != nil && (result.RestartRequired || result.Outcome != "confirmed") {
		writeError(w, 502, "outcome_unknown", "The IAM operation was not safely confirmed. Verify the exact target before making another request.")
		return
	}
	if result.RestartRequired {
		defer s.Close()
	} else {
		// Responses are flushed before the handler returns. Resume requests
		// before acknowledging a change that keeps the selected identity valid.
		finishWrite()
	}
	writeJSON(w, 200, result)
}

func nativeIAMActionAllowed(action string) bool {
	return action == "service-account.enable" || action == "service-account.disable" || action == "service-account.rotate" || action == "service-account.policy" || action == "service-account.delete" || action == "group.add-members" || action == "group.remove-members" || action == "group.enable" || action == "group.disable" || action == "group.delete" || action == "user.rotate" || action == "user.policies" || action == "group.policies" || action == "user.delete" || action == "user.disable" || action == "user.enable" || action == "user.create" || action == "group.create" || action == "service-account.create"
}

func (s *Server) writeNativeRelationshipPlanError(w http.ResponseWriter, err error) {
	if errors.Is(err, errUnknownIdentityScope) {
		writeError(w, 403, "identity_changes_disabled", "This server cannot safely establish the identity relationships for this change.")
		return
	}
	writeBackendError(w, err)
}
