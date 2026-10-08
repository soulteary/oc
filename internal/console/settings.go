// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/soulteary/mc/internal/consoleapi"
)

const maxSettingDocument = 1 << 20

// Done closes when sessions are retired, including after secret rotation.
// The owner must stop its HTTP listener and drain owned tasks at this point.
func (s *Server) Done() <-chan struct{} { return s.life.Done() }

func (s *Server) isSettingsRoute(path string) bool {
	return path == "/api/bucket-settings" || path == "/api/self-account" || path == "/api/self-secret"
}

func (s *Server) serveSettings(w http.ResponseWriter, r *http.Request) {
	method := http.MethodGet
	if r.URL.Path == "/api/self-secret" || (r.URL.Path == "/api/bucket-settings" && r.Method == http.MethodPost) {
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
			writeError(w, 403, "writes_disabled", "Restart OC with writes explicitly enabled to change settings.")
			return
		}
	}
	if s.settings == nil {
		writeError(w, 501, "settings_unsupported", "This connection does not support account or bucket settings.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	stop := context.AfterFunc(sess.ctx, cancel)
	defer func() { stop(); cancel() }()
	r = r.WithContext(ctx)
	if !acquire(w, ctx, s.apiSlots) {
		return
	}
	defer func() { <-s.apiSlots }()
	if r.URL.Path == "/api/bucket-settings" && method == http.MethodGet {
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(r.URL.RawQuery) > maxQueryLength || len(q) != 2 || len(q["bucket"]) != 1 || len(q["kind"]) != 1 || !validSettingTarget(q.Get("bucket"), q.Get("kind")) {
			writeError(w, 400, "invalid_input", "Choose one exact bucket and a setting type.")
			return
		}
		setting, err := s.settings.BucketSetting(ctx, q.Get("bucket"), q.Get("kind"))
		if err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, 200, setting)
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_input", "This request does not accept URL parameters.")
		return
	}
	switch r.URL.Path {
	case "/api/self-account":
		account, err := s.settings.SelfAccount(ctx)
		if err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, 200, account)
	case "/api/bucket-settings":
		s.saveBucketSetting(w, r, sess)
	case "/api/self-secret":
		s.rotateOwnSecret(w, r, sess)
	}
}

func validSettingTarget(bucket, kind string) bool {
	return validBucket(bucket) && (kind == "policy" || kind == "versioning" || kind == "lifecycle")
}

func validSettingRevision(revision string) bool {
	if len(revision) != 64 {
		return false
	}
	for _, ch := range revision {
		if !(ch >= '0' && ch <= '9') && !(ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

// Mutations acquire their slots and enter the wait group under the same lock
// used by Close. Rotation reserves every write slot, so it cannot invalidate
// the identity while an object write or its multipart cleanup is in progress.
func (s *Server) startSettingsWrite(w http.ResponseWriter, sess *session, rotate bool) bool {
	s.mu.Lock()
	if s.closed || s.rotating || sess.ctx.Err() != nil {
		s.mu.Unlock()
		writeError(w, 409, "credential_change_pending", "This connection is ending. Restart OC before writing again.")
		return false
	}
	n := 1
	if rotate {
		n = cap(s.writeSlots)
	}
	for i := 0; i < n; i++ {
		if err := tryAcquire(sess.ctx, s.writeSlots); err != nil {
			for j := 0; j < i; j++ {
				<-s.writeSlots
			}
			s.mu.Unlock()
			if rotate {
				writeError(w, 409, "active_tasks", "Complete or cancel active storage writes before changing the account secret.")
			} else {
				writeSlotError(w, err)
			}
			return false
		}
	}
	s.rotating = rotate
	s.workers.Add(1)
	s.mu.Unlock()
	return true
}

func (s *Server) finishSettingsWrite(rotate bool) {
	n := 1
	if rotate {
		n = cap(s.writeSlots)
	}
	s.mu.Lock()
	for i := 0; i < n; i++ {
		<-s.writeSlots
	}
	if rotate {
		s.rotating = false
	}
	s.mu.Unlock()
	s.workers.Done()
}

func (s *Server) saveBucketSetting(w http.ResponseWriter, r *http.Request, sess *session) {
	var args struct {
		Bucket   string `json:"bucket"`
		Kind     string `json:"kind"`
		Document string `json:"document"`
		Revision string `json:"revision"`
		Remove   bool   `json:"remove"`
		Confirm  bool   `json:"confirm"`
	}
	// A JSON string can expand to six bytes for each document byte.
	if !decodeJSON(w, r, &args, 6*maxSettingDocument+1024) {
		return
	}
	if !args.Confirm || !validSettingTarget(args.Bucket, args.Kind) || !validSettingRevision(args.Revision) || len(args.Document) > maxSettingDocument || !utf8.ValidString(args.Document) || (args.Remove && (args.Kind == "versioning" || args.Document != "")) || (!args.Remove && args.Document == "") {
		writeError(w, 400, "invalid_input", "Confirm a complete configuration and its current revision for one exact bucket.")
		return
	}
	if !s.startSettingsWrite(w, sess, false) {
		return
	}
	defer s.finishSettingsWrite(false)
	setting, err := s.settings.SaveBucketSetting(r.Context(), args.Bucket, args.Kind, args.Document, args.Revision, args.Remove)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, 200, setting)
}

func (s *Server) rotateOwnSecret(w http.ResponseWriter, r *http.Request, sess *session) {
	var args struct {
		NewSecret string `json:"newSecret"`
		Confirm   bool   `json:"confirm"`
	}
	if !decodeJSON(w, r, &args, 1024) {
		return
	}
	if !args.Confirm || !utf8.ValidString(args.NewSecret) || len(args.NewSecret) < 8 || len(args.NewSecret) > 128 || strings.ContainsAny(args.NewSecret, "\x00\r\n") {
		writeError(w, 400, "invalid_input", "Confirm an account secret between 8 and 128 UTF-8 bytes.")
		return
	}
	if !s.startSettingsWrite(w, sess, true) {
		return
	}
	defer s.finishSettingsWrite(true)
	err := s.settings.RotateOwnSecret(r.Context(), args.NewSecret)
	args.NewSecret = ""
	if err != nil {
		var apiError *consoleapi.Error
		if errors.As(err, &apiError) && apiError != nil && apiError.Code != "outcome_unknown" {
			status := apiError.Status
			if status < 400 || status > 599 {
				status = http.StatusBadGateway
			}
			writeJSON(w, status, struct {
				*consoleapi.Error
				RestartRequired bool `json:"restartRequired"`
			}{apiError, false})
			return
		}
		// Even a broken browser connection must retire the old credentials.
		// Close is deferred until the acknowledgement has been flushed.
		defer s.Close()
		writeJSON(w, 502, struct {
			Code            string `json:"code"`
			Message         string `json:"message"`
			RestartRequired bool   `json:"restartRequired"`
		}{"outcome_unknown", "The secret change may have completed. Update the alias in your terminal and restart OC before continuing.", true})
		return
	}
	defer s.Close()
	writeJSON(w, 200, struct {
		RestartRequired bool   `json:"restartRequired"`
		Outcome         string `json:"outcome"`
	}{true, "confirmed"})
}
