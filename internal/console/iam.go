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
		writeError(w, 401, "login_required", "Sign in with the code printed by OC.")
		return
	}
	if method == http.MethodPost {
		if !s.requireOrigin(w, r) || !s.requireCSRF(w, r, sess) {
			return
		}
		if s.writer == nil {
			writeError(w, 403, "writes_disabled", "Restart OC with writes explicitly enabled to manage IAM.")
			return
		}
	}
	backend, ok := s.backend.(consoleapi.IAMBackend)
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
		s.iamAction(w, r, sess, backend)
		return
	}
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, 200, value)
}

func (s *Server) iamAction(w http.ResponseWriter, r *http.Request, sess *session, backend consoleapi.IAMBackend) {
	var args consoleapi.IAMActionRequest
	if !decodeJSON(w, r, &args, 6*20*1024+8192) {
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
	// Any IAM change can alter the selected identity, directly or through a
	// parent/group. Pause other requests and reserve all storage write slots
	// before preflight, so active tasks cannot outlive invalidated credentials.
	if !s.startSettingsWrite(w, sess, true) {
		return
	}
	defer s.finishSettingsWrite(true)
	result, err := backend.IAMAction(r.Context(), args)
	args.SecretKey = ""
	if err != nil {
		var apiError *consoleapi.Error
		if errors.As(err, &apiError) && apiError != nil && apiError.Code != "outcome_unknown" {
			writeBackendError(w, apiError)
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
	if result.RestartRequired {
		defer s.Close()
	}
	writeJSON(w, 200, result)
}
