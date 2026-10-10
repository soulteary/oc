// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/soulteary/mc/internal/consoleapi"
)

func (s *Server) isFeatureRoute(path string) bool {
	return path == "/api/objects/rename" || path == "/api/buckets/create" || path == "/api/buckets/delete" || path == "/api/versions" || path == "/api/shares"
}

func (s *Server) serveFeatures(w http.ResponseWriter, r *http.Request) {
	method := http.MethodPost
	if r.URL.Path == "/api/versions" {
		method = http.MethodGet
	}
	if !s.requireMethod(w, r, method) {
		return
	}
	sess, _ := s.authenticate(r)
	if sess == nil {
		writeError(w, 401, "login_required", "Sign in with the code printed by OC.")
		return
	}
	if method == http.MethodPost && (!s.requireOrigin(w, r) || !s.requireCSRF(w, r, sess)) {
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
	if r.URL.Path == "/api/versions" {
		s.listVersions(w, r)
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_input", "This request does not accept URL parameters.")
		return
	}
	if r.URL.Path == "/api/shares" {
		s.createShare(w, r)
		return
	}
	if r.URL.Path == "/api/objects/rename" {
		s.renameObject(w, r, sess)
		return
	}
	if s.writer == nil {
		writeError(w, 403, "writes_disabled", "Restart OC with writes explicitly enabled to manage buckets.")
		return
	}
	backend, ok := s.backend.(consoleapi.BucketBackend)
	if !ok {
		writeError(w, 501, "buckets_unsupported", "This connection does not support bucket management.")
		return
	}
	var args struct {
		Bucket        string `json:"bucket"`
		ConfirmBucket string `json:"confirmBucket"`
	}
	if !decodeJSON(w, r, &args, 1024) {
		return
	}
	deleting := r.URL.Path == "/api/buckets/delete"
	if !validNewBucket(args.Bucket) || (deleting && args.ConfirmBucket != args.Bucket) || (!deleting && args.ConfirmBucket != "") {
		writeError(w, 400, "invalid_input", "Choose a valid bucket name. To delete an empty bucket, confirm its exact name.")
		return
	}
	if !s.startSettingsWrite(w, sess, false) {
		return
	}
	defer s.finishSettingsWrite(false)
	var err error
	if deleting {
		err = backend.DeleteBucket(ctx, args.Bucket)
	} else {
		err = backend.CreateBucket(ctx, args.Bucket)
	}
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, 200, struct {
		Bucket  string `json:"bucket"`
		Outcome string `json:"outcome"`
	}{args.Bucket, "confirmed"})
}

func validNewBucket(bucket string) bool {
	if !validBucket(bucket) || len(bucket) < 3 || net.ParseIP(bucket) != nil || strings.Contains(bucket, "..") {
		return false
	}
	for _, label := range strings.Split(bucket, ".") {
		if label == "" || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}

func validVersionID(version string) bool {
	return len(version) <= 1024 && utf8.ValidString(version) && !strings.ContainsAny(version, "\x00\r\n")
}

func validObjectRef(ref consoleapi.ObjectRef) bool {
	return validBucket(ref.Bucket) && ref.Key != "" && validKey(ref.Key) && validVersionID(ref.VersionID)
}

func (s *Server) listVersions(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(r.URL.RawQuery) > maxQueryLength || len(q) < 2 || len(q) > 4 || len(q["bucket"]) != 1 || len(q["key"]) != 1 || len(q["cursor"]) > 1 || len(q["limit"]) > 1 {
		writeError(w, 400, "invalid_input", "Choose one exact object and a valid version page.")
		return
	}
	for field := range q {
		if field != "bucket" && field != "key" && field != "cursor" && field != "limit" {
			writeError(w, 400, "invalid_input", "The version request contains unsupported parameters.")
			return
		}
	}
	ref := consoleapi.ObjectRef{Bucket: q.Get("bucket"), Key: q.Get("key")}
	limit := 100
	if q.Get("limit") != "" {
		limit, err = strconv.Atoi(q.Get("limit"))
	}
	if err != nil || !validObjectRef(ref) || limit < 1 || limit > 1000 || len(q.Get("cursor")) > 16*1024 || !utf8.ValidString(q.Get("cursor")) {
		writeError(w, 400, "invalid_input", "Choose one exact object and a valid version page.")
		return
	}
	backend, ok := s.backend.(consoleapi.VersionBackend)
	if !ok {
		writeError(w, 501, "versions_unsupported", "This connection does not support object version history.")
		return
	}
	page, err := backend.ListVersions(r.Context(), ref.Bucket, ref.Key, q.Get("cursor"), limit)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	if page.Entries == nil {
		page.Entries = []consoleapi.VersionEntry{}
	}
	writeJSON(w, 200, page)
}

func validShareName(name string) bool {
	return len(name) <= 255 && utf8.ValidString(name) && !strings.ContainsAny(name, "\x00\r\n/\\") && name != "." && name != ".."
}

func (s *Server) createShare(w http.ResponseWriter, r *http.Request) {
	if !s.allowSharing {
		writeError(w, 403, "sharing_disabled", "Restart OC with sharing explicitly enabled to create download links.")
		return
	}
	backend, ok := s.backend.(consoleapi.ShareBackend)
	if !ok {
		writeError(w, 501, "sharing_unsupported", "This connection does not support signed download links.")
		return
	}
	var args consoleapi.ShareRequest
	if !decodeJSON(w, r, &args, 8*1024) {
		return
	}
	if args.ExpiresSeconds == 0 {
		args.ExpiresSeconds = 60 * 60
	}
	if !validObjectRef(args.ObjectRef) || args.ExpiresSeconds < 1 || args.ExpiresSeconds > 7*24*60*60 || !validShareName(args.DownloadName) {
		writeError(w, 400, "invalid_input", "Choose one exact object and an expiry between one second and seven days.")
		return
	}
	share, err := backend.Presign(r.Context(), args)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, 200, share)
}

func (s *Server) renameObject(w http.ResponseWriter, r *http.Request, sess *session) {
	if s.writer == nil {
		writeError(w, 403, "writes_disabled", "Writes are disabled.")
		return
	}
	backend, ok := s.backend.(consoleapi.RenameBackend)
	if !ok {
		writeError(w, 501, "rename_unsupported", "This connection does not support renaming.")
		return
	}
	var args struct {
		Bucket string `json:"bucket"`
		Key    string `json:"key"`
		NewKey string `json:"newKey"`
		ETag   string `json:"etag"`
	}
	if !decodeJSON(w, r, &args, 4096) {
		return
	}
	if !validBucket(args.Bucket) || args.Key == "" || args.NewKey == "" || !validKey(args.Key) || !validKey(args.NewKey) || args.Key == args.NewKey || args.ETag == "" {
		writeError(w, 400, "invalid_input", "Choose a different valid object name.")
		return
	}
	if !s.startSettingsWrite(w, sess, false) {
		return
	}
	defer s.finishSettingsWrite(false)
	if err := backend.RenameObject(r.Context(), args.Bucket, args.Key, args.NewKey, args.ETag); err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"outcome": "confirmed"})
}
