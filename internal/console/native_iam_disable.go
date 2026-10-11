// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package console

import (
	"errors"
	"net/http"

	"github.com/soulteary/mc/internal/consoleapi"
)

// Quiesce writes before disabling an account, including self-targeted changes.
// Gate only the target user's new requests and sign-ins. Existing read requests
// are canceled when its sessions are retired after the acknowledgement.
func (s *Server) disableNativeUser(w http.ResponseWriter, r *http.Request, sess *session, backend consoleapi.IAMBackend, args consoleapi.IAMActionRequest) {
	if !s.startSettingsWrite(w, sess, true) {
		return
	}
	s.mu.Lock()
	s.nativeUserChanging[args.User] = true
	s.mu.Unlock()
	revoke := false
	finished := false
	finish := func() {
		if finished {
			return
		}
		finished = true
		s.mu.Lock()
		if revoke {
			s.nativeEpoch++
			if s.nativeLogins > 0 {
				s.nativeUserRevoked[args.User] = s.nativeEpoch
			}
			for token, other := range s.sessions {
				if other.nativeUser == args.User {
					s.removeSessionLocked(token, other)
				}
			}
		}
		delete(s.nativeUserChanging, args.User)
		s.mu.Unlock()
		s.finishSettingsWrite(true)
	}
	defer finish()
	result, err := backend.IAMAction(r.Context(), args)
	if err != nil {
		var failure *consoleapi.Error
		if errors.As(err, &failure) && failure != nil && failure.Code != "outcome_unknown" {
			finish()
			writeBackendError(w, failure)
			return
		}
	}
	// A lost or inconsistent acknowledgement can still have disabled the user.
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
		}{"outcome_unknown", "The account may have been disabled. Verify its status before another change. Its existing sessions will require a fresh login.", loginRequired})
		return
	}
	// A self-target status change never stops the shared native console.
	result.RestartRequired = false
	result.Credentials = nil
	writeJSON(w, 200, struct {
		consoleapi.IAMActionResult
		LoginRequired bool `json:"loginRequired"`
	}{result, loginRequired})
}
