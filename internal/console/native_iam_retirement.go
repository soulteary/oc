// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"errors"
	"net/http"

	"github.com/soulteary/mc/internal/consoleapi"
)

// Quiesce writes before disabling or deleting an account, including self-targeted changes.
// Gate only the target user's new requests and sign-ins. Existing read requests
// are canceled when its sessions are retired after the acknowledgement.
func (s *Server) retireNativeUser(w http.ResponseWriter, r *http.Request, sess *session, backend consoleapi.IAMBackend, args consoleapi.IAMActionRequest) {
	mutation := s.beginIdentityMutation(w, sess, directIdentityImpact(nativeUserScope(args.User)))
	if mutation == nil {
		return
	}
	revoke := false
	finish := func() { mutation.finish(revoke) }
	defer finish()
	result, err := backend.IAMAction(r.Context(), args)
	if err != nil {
		var failure *consoleapi.Error
		if errors.As(err, &failure) && failure != nil && failure.Code != "outcome_unknown" {
			// An absent delete target must not regain its old cookies if the
			// same credentials are recreated before the next request.
			if args.Action == "user.delete" && failure.Code == "iam_not_found" {
				revoke = true
				loginRequired := sess.nativeUser == args.User
				if !loginRequired {
					finish()
				}
				writeJSON(w, http.StatusNotFound, struct {
					Code          string `json:"code"`
					Message       string `json:"message"`
					LoginRequired bool   `json:"loginRequired"`
				}{failure.Code, failure.Message, loginRequired})
				return
			}
			finish()
			writeBackendError(w, failure)
			return
		}
	}
	// A lost or inconsistent acknowledgement can still have disabled or deleted the user.
	// Retire only that user's sessions even if the requester has disconnected.
	revoke = true
	loginRequired := sess.nativeUser == args.User
	if !loginRequired {
		finish()
	}
	if err != nil || result.Outcome != "confirmed" {
		writeJSON(w, 502, struct {
			Code          string `json:"code"`
			Message       string `json:"message"`
			LoginRequired bool   `json:"loginRequired"`
		}{"outcome_unknown", "The account may have been disabled or deleted. Verify its status before another change. Its existing sessions will require a fresh login.", loginRequired})
		return
	}
	// A self-target identity change never stops the shared native console.
	result.RestartRequired = false
	result.Credentials = nil
	writeJSON(w, 200, struct {
		consoleapi.IAMActionResult
		LoginRequired bool `json:"loginRequired"`
	}{result, loginRequired})
}
