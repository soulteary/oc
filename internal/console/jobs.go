// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

const maxPlanKeys = 1000
const maxJobJSON = 2 << 20

type job struct {
	info    consoleapi.Job
	owner   *session
	ctx     context.Context
	cancel  context.CancelFunc
	timer   *time.Timer
	confirm [32]byte
}

// Wait waits for active storage mutations after Close has canceled sessions.
func (s *Server) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() { s.workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) requireCSRF(w http.ResponseWriter, r *http.Request, sess *session) bool {
	csrf := r.Header.Get("X-CSRF-Token")
	if len(r.Header.Values("X-CSRF-Token")) != 1 || csrf == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(sess.csrf)) != 1 {
		writeError(w, 403, "invalid_csrf", "The session security token is invalid.")
		return false
	}
	return true
}

func (s *Server) isJobRoute(path string) bool {
	return path == "/api/uploads" || path == "/api/jobs" || path == "/api/deletions/plan" || strings.HasPrefix(path, "/api/uploads/") || strings.HasPrefix(path, "/api/jobs/") || strings.HasPrefix(path, "/api/deletions/")
}

func (s *Server) serveJobs(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := http.MethodPost
	id := ""
	action := ""
	switch {
	case path == "/api/uploads":
		action = "new-upload"
	case path == "/api/deletions/plan":
		action = "plan"
	case path == "/api/jobs":
		method = http.MethodGet
		action = "list"
	case strings.HasPrefix(path, "/api/uploads/"):
		id = strings.TrimPrefix(path, "/api/uploads/")
		method = http.MethodPut
		action = "upload"
	case strings.HasPrefix(path, "/api/deletions/") && strings.HasSuffix(path, "/execute"):
		id = strings.TrimSuffix(strings.TrimPrefix(path, "/api/deletions/"), "/execute")
		action = "execute"
	case strings.HasPrefix(path, "/api/jobs/"):
		id = strings.TrimPrefix(path, "/api/jobs/")
		method = http.MethodGet
		action = "status"
		if strings.HasSuffix(id, "/cancel") {
			id = strings.TrimSuffix(id, "/cancel")
			method = http.MethodPost
			action = "cancel"
		}
	default:
		writeError(w, 404, "not_found", "This console endpoint does not exist.")
		return
	}
	if id != "" && (len(id) != 43 || strings.Contains(id, "/")) {
		writeError(w, 404, "not_found", "This task does not exist.")
		return
	}
	if !s.requireMethod(w, r, method) {
		return
	}
	sess, _ := s.authenticate(r)
	if sess == nil {
		writeError(w, 401, "login_required", "Sign in with the code printed by OC.")
		return
	}
	if method != http.MethodGet && (!s.requireOrigin(w, r) || !s.requireCSRF(w, r, sess)) {
		return
	}
	if s.writer == nil {
		writeError(w, 403, "writes_disabled", "Restart OC with writes explicitly enabled to use storage tasks.")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(sess.ctx, cancel)
	defer func() { stop(); cancel() }()
	r = r.WithContext(ctx)
	if len(r.URL.RawQuery) > 0 {
		writeError(w, 400, "invalid_input", "Task requests do not accept URL parameters.")
		return
	}
	switch action {
	case "new-upload":
		s.newUpload(w, r, sess)
	case "plan":
		s.planDeletion(w, r, sess)
	case "list":
		s.mu.Lock()
		jobs := []consoleapi.Job{}
		for _, j := range s.jobs {
			if j.owner == sess {
				jobs = append(jobs, snapshot(j))
			}
		}
		s.mu.Unlock()
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].Created.Before(jobs[j].Created) })
		writeJSON(w, 200, struct {
			Jobs []consoleapi.Job `json:"jobs"`
		}{jobs})
	case "status":
		s.mu.Lock()
		j := s.ownedJobLocked(id, sess)
		if j == nil {
			s.mu.Unlock()
			writeError(w, 404, "not_found", "This task does not exist.")
			return
		}
		info := snapshot(j)
		s.mu.Unlock()
		writeJSON(w, 200, info)
	case "cancel":
		s.cancelJob(w, sess, id)
	case "upload":
		s.uploadJob(w, r, sess, id)
	case "execute":
		s.executeDeletion(w, r, sess, id)
	}
}

func snapshot(j *job) consoleapi.Job {
	info := j.info
	info.ConfirmToken = ""
	info.Items = append([]consoleapi.JobItem(nil), info.Items...)
	if info.Error != nil {
		e := *info.Error
		info.Error = &e
	}
	for i := range info.Items {
		if info.Items[i].Error != nil {
			e := *info.Items[i].Error
			info.Items[i].Error = &e
		}
	}
	return info
}

func (s *Server) ownedJobLocked(id string, sess *session) *job {
	j := s.jobs[id]
	if j == nil || j.owner != sess {
		return nil
	}
	return j
}

func (s *Server) createJob(w http.ResponseWriter, sess *session, info consoleapi.Job, ttl time.Duration) *job {
	id, err := randomToken()
	if err != nil {
		writeError(w, 500, "internal_error", "Unable to create a task.")
		return nil
	}
	s.mu.Lock()
	if s.closed || sess.ctx.Err() != nil {
		s.mu.Unlock()
		writeError(w, 401, "login_required", "The console session has ended.")
		return nil
	}
	count := 0
	for _, j := range s.jobs {
		if j.owner == sess {
			count++
		}
	}
	if (count >= 16 && !s.evictTerminalLocked(sess)) || (len(s.jobs) >= 64 && !s.evictTerminalLocked(nil)) {
		s.mu.Unlock()
		writeError(w, 429, "too_many_tasks", "Too many active tasks are open. Complete or cancel an existing task first.")
		return nil
	}
	ctx, cancel := context.WithCancel(sess.ctx)
	info.ID = id
	info.Created = time.Now()
	info.Expires = info.Created.Add(ttl)
	j := &job{info: info, owner: sess, ctx: ctx, cancel: cancel}
	s.jobs[id] = j
	j.timer = time.AfterFunc(ttl, func() { s.expireJob(j) })
	s.mu.Unlock()
	return j
}

func (s *Server) evictTerminalLocked(owner *session) bool {
	var oldest *job
	for _, j := range s.jobs {
		if !terminal(j.info.Status) || (owner != nil && j.owner != owner) {
			continue
		}
		if oldest == nil || j.info.Created.Before(oldest.info.Created) {
			oldest = j
		}
	}
	if oldest == nil {
		return false
	}
	oldest.timer.Stop()
	delete(s.jobs, oldest.info.ID)
	return true
}

func terminal(status string) bool {
	return status == "succeeded" || status == "failed" || status == "partial" || status == "canceled"
}

func (s *Server) expireJob(j *job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs[j.info.ID] != j {
		return
	}
	// A stopped timer callback may already be queued behind the mutex. Never
	// let the previous waiting/ready timer cancel a task that has since started.
	if time.Now().Before(j.info.Expires) {
		return
	}
	if terminal(j.info.Status) {
		delete(s.jobs, j.info.ID)
		return
	}
	j.cancel()
	if j.info.Status == "waiting" || j.info.Status == "ready" {
		s.finishJobLocked(j, "canceled", &consoleapi.Error{Code: "task_expired", Message: "The task expired before it started."})
	}
}

func (s *Server) finishJobLocked(j *job, status string, err *consoleapi.Error) {
	j.info.Status = status
	j.info.Error = err
	j.confirm = [32]byte{}
	j.timer.Stop()
	j.cancel()
	j.info.Expires = time.Now().Add(10 * time.Minute)
	if s.jobs[j.info.ID] == j {
		j.timer = time.AfterFunc(10*time.Minute, func() { s.expireJob(j) })
	}
}

func safeJobError(err error) *consoleapi.Error {
	var safe *consoleapi.Error
	if errors.As(err, &safe) && safe != nil {
		copy := *safe
		return &copy
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &consoleapi.Error{Code: "request_canceled", Message: "The request stopped. Its storage result may be unknown; refresh the object list before retrying."}
	}
	return &consoleapi.Error{Code: "upstream_error", Message: "The storage request failed. Its result may be unknown; refresh the object list before retrying."}
}

func (s *Server) newUpload(w http.ResponseWriter, r *http.Request, sess *session) {
	var args struct {
		Bucket    string `json:"bucket"`
		Key       string `json:"key"`
		Size      int64  `json:"size"`
		Overwrite bool   `json:"overwrite"`
	}
	if !decodeJSON(w, r, &args, maxLoginBody*4) {
		return
	}
	if !validBucket(args.Bucket) || args.Key == "" || !validKey(args.Key) || args.Size < 0 || args.Size > s.maxUploadSize {
		writeError(w, 400, "invalid_input", "The bucket, key or file size is invalid or exceeds the upload limit.")
		return
	}
	j := s.createJob(w, sess, consoleapi.Job{Kind: "upload", Status: "waiting", Bucket: args.Bucket, Key: args.Key, Size: args.Size, Overwrite: args.Overwrite, Count: 1}, time.Minute)
	if j == nil {
		return
	}
	s.mu.Lock()
	info := snapshot(j)
	s.mu.Unlock()
	writeJSON(w, 201, info)
}

func (s *Server) cancelJob(w http.ResponseWriter, sess *session, id string) {
	s.mu.Lock()
	j := s.ownedJobLocked(id, sess)
	if j == nil {
		s.mu.Unlock()
		writeError(w, 404, "not_found", "This task does not exist.")
		return
	}
	if !terminal(j.info.Status) {
		j.cancel()
		if j.info.Status == "waiting" || j.info.Status == "ready" {
			s.finishJobLocked(j, "canceled", nil)
		}
	}
	info := snapshot(j)
	s.mu.Unlock()
	writeJSON(w, 200, info)
}

type countingReader struct {
	reader io.Reader
	read   int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

func (s *Server) uploadJob(w http.ResponseWriter, r *http.Request, sess *session, id string) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/octet-stream" {
		writeError(w, 415, "invalid_input", "Upload the raw file as application/octet-stream.")
		return
	}
	s.mu.Lock()
	j := s.ownedJobLocked(id, sess)
	if j == nil {
		s.mu.Unlock()
		writeError(w, 404, "not_found", "This task does not exist.")
		return
	}
	if j.info.Kind != "upload" || j.info.Status != "waiting" || j.ctx.Err() != nil || !time.Now().Before(j.info.Expires) {
		s.mu.Unlock()
		writeError(w, 409, "task_state", "This upload cannot be started again.")
		return
	}
	if r.ContentLength != j.info.Size {
		s.mu.Unlock()
		writeError(w, 400, "invalid_input", "The upload length does not match the registered file size.")
		return
	}
	if err := tryAcquire(j.ctx, s.writeSlots); err != nil {
		s.mu.Unlock()
		writeSlotError(w, err)
		return
	}
	j.timer.Stop()
	j.info.Status = "running"
	j.info.Expires = j.owner.expires
	s.workers.Add(1)
	s.mu.Unlock()
	defer func() { <-s.writeSlots; s.workers.Done() }()
	ctx, cancel := context.WithCancel(j.ctx)
	stop := context.AfterFunc(r.Context(), cancel)
	defer func() { stop(); cancel() }()
	body, closeBody := boundedBody(w, r, ctx, s.streamIdle)
	defer closeBody()
	counter := &countingReader{reader: io.LimitReader(body, j.info.Size)}
	result, uploadErr := s.writer.Upload(ctx, j.info.Bucket, j.info.Key, counter, j.info.Size, consoleapi.UploadOptions{Overwrite: j.info.Overwrite}, func(n int64) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if n > j.info.Transferred {
			if n > j.info.Size {
				n = j.info.Size
			}
			j.info.Transferred = n
		}
	})
	if uploadErr == nil && (counter.read != j.info.Size || result.Size != j.info.Size) {
		uploadErr = io.ErrUnexpectedEOF
	}
	s.mu.Lock()
	if uploadErr == nil {
		j.info.ETag = result.ETag
		j.info.Transferred = j.info.Size
		j.info.Completed = 1
		s.finishJobLocked(j, "succeeded", nil)
	} else {
		status := "failed"
		if ctx.Err() != nil {
			status = "canceled"
		}
		s.finishJobLocked(j, status, safeJobError(uploadErr))
	}
	info := snapshot(j)
	s.mu.Unlock()
	writeJSON(w, 200, info)
}

func (s *Server) planDeletion(w http.ResponseWriter, r *http.Request, sess *session) {
	var args struct {
		Bucket string    `json:"bucket"`
		Keys   *[]string `json:"keys"`
		Prefix *string   `json:"prefix"`
	}
	if !decodeJSON(w, r, &args, maxJobJSON) {
		return
	}
	if !validBucket(args.Bucket) || (args.Keys == nil) == (args.Prefix == nil) {
		writeError(w, 400, "invalid_input", "Specify one bucket and either object keys or a directory prefix.")
		return
	}
	keys := []string{}
	prefix := ""
	seen := map[string]bool{}
	if args.Keys != nil {
		if len(*args.Keys) == 0 || len(*args.Keys) > maxPlanKeys {
			writeError(w, 400, "invalid_input", "Select between one and 1000 distinct object keys.")
			return
		}
		for _, key := range *args.Keys {
			if key == "" || !validKey(key) || seen[key] {
				writeError(w, 400, "invalid_input", "Object keys must be valid and distinct.")
				return
			}
			seen[key] = true
			keys = append(keys, key)
		}
	} else {
		prefix = *args.Prefix
		if prefix == "" || !validKey(prefix) || !strings.HasSuffix(prefix, "/") {
			writeError(w, 400, "invalid_input", "A directory prefix must be nonempty and end with a slash.")
			return
		}
		if !acquire(w, r.Context(), s.planSlots) {
			return
		}
		defer func() { <-s.planSlots }()
	}
	j := s.createJob(w, sess, consoleapi.Job{Kind: "delete", Status: "planning", Bucket: args.Bucket, Prefix: prefix}, 60*time.Second)
	if j == nil {
		return
	}
	ctx, cancel := context.WithTimeout(j.ctx, 60*time.Second)
	stop := context.AfterFunc(r.Context(), cancel)
	defer func() { stop(); cancel() }()
	var planErr error
	if prefix != "" {
		cursor := ""
		cursors := map[string]bool{}
		for pageNum := 0; pageNum <= maxPlanKeys; pageNum++ {
			limit := maxPlanKeys - len(keys)
			if limit < 1 {
				limit = 1
			}
			page, err := s.writer.ScanObjects(ctx, args.Bucket, prefix, cursor, limit)
			if err != nil {
				planErr = err
				break
			}
			for _, entry := range page.Entries {
				if entry.IsPrefix || entry.Key == "" || !validKey(entry.Key) || !strings.HasPrefix(entry.Key, prefix) || seen[entry.Key] {
					planErr = &consoleapi.Error{Code: "invalid_listing", Message: "The storage listing was inconsistent; no deletion has started."}
					break
				}
				seen[entry.Key] = true
				keys = append(keys, entry.Key)
				if len(keys) > maxPlanKeys {
					planErr = &consoleapi.Error{Code: "plan_too_large", Message: "This directory exceeds the 1000 object deletion limit. No deletion has started."}
					break
				}
			}
			if planErr != nil || page.NextCursor == "" {
				break
			}
			if cursors[page.NextCursor] || page.NextCursor == cursor || pageNum == maxPlanKeys {
				planErr = &consoleapi.Error{Code: "invalid_listing", Message: "The storage listing did not finish; no deletion has started."}
				break
			}
			cursors[page.NextCursor] = true
			cursor = page.NextCursor
		}
	}
	if planErr == nil {
		planErr = ctx.Err()
	}
	confirm := ""
	if planErr == nil {
		confirm, planErr = randomToken()
	}
	s.mu.Lock()
	// Logout, expiry or Close may win after the final list page but before
	// acquiring this lock. Do not resurrect a removed plan or its timer.
	if planErr == nil && (s.jobs[j.info.ID] != j || j.ctx.Err() != nil || ctx.Err() != nil) {
		planErr = context.Canceled
	}
	if planErr != nil {
		status := "failed"
		if j.ctx.Err() != nil || ctx.Err() != nil {
			status = "canceled"
		}
		planError := safeJobError(planErr)
		var safe *consoleapi.Error
		if errors.As(planErr, &safe) && safe != nil {
			planError.Message += " No deletion has started."
		} else {
			planError.Message = "Unable to finish the deletion plan. No deletion has started."
		}
		s.finishJobLocked(j, status, planError)
	} else {
		j.info.Count = len(keys)
		j.info.Items = make([]consoleapi.JobItem, len(keys))
		for i, key := range keys {
			j.info.Items[i] = consoleapi.JobItem{Key: key, Status: "pending"}
		}
		j.info.Status = "ready"
		j.info.Expires = time.Now().Add(10 * time.Minute)
		j.confirm = sha256.Sum256([]byte(confirm))
		j.timer.Stop()
		j.timer = time.AfterFunc(10*time.Minute, func() { s.expireJob(j) })
	}
	info := snapshot(j)
	if planErr == nil {
		info.ConfirmToken = confirm
	}
	s.mu.Unlock()
	writeJSON(w, 200, info)
}

func (s *Server) executeDeletion(w http.ResponseWriter, r *http.Request, sess *session, id string) {
	var args struct {
		ConfirmToken string `json:"confirmToken"`
	}
	if !decodeJSON(w, r, &args, maxLoginBody) {
		return
	}
	digest := sha256.Sum256([]byte(args.ConfirmToken))
	s.mu.Lock()
	j := s.ownedJobLocked(id, sess)
	if j == nil {
		s.mu.Unlock()
		writeError(w, 404, "not_found", "This task does not exist.")
		return
	}
	if j.info.Kind != "delete" || j.info.Status != "ready" || j.ctx.Err() != nil || !time.Now().Before(j.info.Expires) {
		s.mu.Unlock()
		writeError(w, 409, "task_state", "This deletion plan is no longer ready.")
		return
	}
	if args.ConfirmToken == "" || subtle.ConstantTimeCompare(digest[:], j.confirm[:]) != 1 {
		s.mu.Unlock()
		writeError(w, 403, "invalid_confirmation", "The deletion confirmation is invalid.")
		return
	}
	if j.info.Count == 0 {
		s.finishJobLocked(j, "succeeded", nil)
		info := snapshot(j)
		s.mu.Unlock()
		writeJSON(w, 200, info)
		return
	}
	if err := tryAcquire(j.ctx, s.writeSlots); err != nil {
		s.mu.Unlock()
		writeSlotError(w, err)
		return
	}
	j.confirm = [32]byte{}
	j.info.Status = "running"
	j.info.Expires = j.owner.expires
	j.timer.Stop()
	s.workers.Add(1)
	info := snapshot(j)
	s.mu.Unlock()
	go s.runDeletion(j)
	writeJSON(w, 202, info)
}

func (s *Server) runDeletion(j *job) {
	defer func() { <-s.writeSlots; s.workers.Done() }()
	failed := false
	for i := range j.info.Items {
		if j.ctx.Err() != nil {
			break
		}
		err := s.writer.DeleteObject(j.ctx, j.info.Bucket, j.info.Items[i].Key)
		s.mu.Lock()
		if err == nil {
			j.info.Items[i].Status = "succeeded"
			j.info.Completed++
		} else {
			failed = true
			j.info.Items[i].Status = "failed"
			j.info.Items[i].Error = safeJobError(err)
			var safe *consoleapi.Error
			if j.ctx.Err() != nil || !errors.As(err, &safe) || safe == nil || safe.Status >= 500 || safe.Status == 408 || safe.Code == "upstream_error" || safe.Code == "request_canceled" || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				j.info.Items[i].Status = "unknown"
			}
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	status := "succeeded"
	var err *consoleapi.Error
	if j.ctx.Err() != nil && j.info.Completed != j.info.Count {
		status = "canceled"
		err = safeJobError(j.ctx.Err())
	} else if failed {
		status = "partial"
		if j.info.Completed == 0 {
			status = "failed"
		}
	}
	s.finishJobLocked(j, status, err)
}
