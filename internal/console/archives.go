// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/soulteary/mc/internal/consoleapi"
)

const archiveTTL = 10 * time.Minute

type archiveReply struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	Count       int               `json:"count"`
	Size        int64             `json:"size"`
	Transferred int64             `json:"transferred"`
	Created     time.Time         `json:"created"`
	Expires     time.Time         `json:"expires"`
	Entries     []archiveItem     `json:"entries,omitempty"`
	Error       *consoleapi.Error `json:"error,omitempty"`
}

type archiveTask struct {
	info   archiveReply
	owner  *session
	ctx    context.Context
	cancel context.CancelFunc
	timer  *time.Timer
	path   string
	slot   bool
}

type archiveItem struct {
	consoleapi.ObjectRef
	Path string `json:"archivePath"`
	Size int64  `json:"size"`
	ETag string `json:"etag"`
}

func (s *Server) isArchiveRoute(path string) bool {
	return path == "/api/archives" || strings.HasPrefix(path, "/api/archives/")
}

func (s *Server) serveArchives(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/archives")
	method, id, action := http.MethodPost, "", "create"
	if path == "" {
		if r.Method == http.MethodGet {
			method, action = http.MethodGet, "list"
		} else if r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET to list archives or POST to prepare one.")
			return
		}
	} else {
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) > 2 || len(parts[0]) != 43 {
			writeError(w, 404, "not_found", "This archive does not exist.")
			return
		}
		id, method, action = parts[0], http.MethodGet, "status"
		if len(parts) == 2 {
			switch parts[1] {
			case "download":
				action = "download"
			case "cancel":
				action, method = "cancel", http.MethodPost
			default:
				writeError(w, 404, "not_found", "This archive does not exist.")
				return
			}
		}
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
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_input", "Archive requests do not accept URL parameters.")
		return
	}
	reader, ok := sess.runtime.backend.(consoleapi.ReferenceBackend)
	if !ok {
		writeError(w, 501, "archives_unsupported", "This connection does not support archives.")
		return
	}
	if action == "list" {
		s.mu.Lock()
		archives := make([]archiveReply, 0)
		for _, task := range s.archives {
			if task.owner == sess {
				archives = append(archives, task.info)
			}
		}
		s.mu.Unlock()
		sort.Slice(archives, func(i, j int) bool {
			if archives[i].Created.Equal(archives[j].Created) {
				return archives[i].ID > archives[j].ID
			}
			return archives[i].Created.After(archives[j].Created)
		})
		writeJSON(w, http.StatusOK, struct {
			Archives []archiveReply `json:"archives"`
		}{archives})
		return
	}
	if action == "create" {
		s.newArchive(w, r, sess, reader)
		return
	}
	s.mu.Lock()
	task := s.archives[id]
	if task == nil || task.owner != sess {
		s.mu.Unlock()
		writeError(w, 404, "not_found", "This archive does not exist.")
		return
	}
	if action == "cancel" {
		s.cancelArchiveLocked(task)
		info := task.info
		s.mu.Unlock()
		writeJSON(w, 200, info)
		return
	}
	if action == "status" {
		info := task.info
		s.mu.Unlock()
		writeJSON(w, 200, info)
		return
	}
	if task.info.Status != "ready" || task.path == "" {
		s.mu.Unlock()
		writeError(w, 409, "archive_not_ready", "The archive is unavailable or is still being prepared.")
		return
	}
	task.info.Status = "downloading"
	filename := task.path
	s.workers.Add(1)
	s.mu.Unlock()
	defer s.workers.Done()
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(task.ctx, cancel)
	defer func() { stop(); cancel() }()
	downloadReserved := false
	defer func() {
		if downloadReserved {
			return
		}
		// Sending a busy/canceled response can itself panic when the browser
		// disconnects. Roll back the task state even on that path, or finish
		// cancellation if its owner revoked the task while the response wrote.
		s.mu.Lock()
		canceled := task.ctx.Err() != nil || task.info.Status == "canceled"
		if !canceled && task.info.Status == "downloading" {
			task.info.Status = "ready"
		}
		s.mu.Unlock()
		if canceled {
			s.finishArchive(task, "canceled", nil)
		}
	}()
	if !acquire(w, ctx, s.downloads) {
		return
	}
	downloadReserved = true
	defer func() { <-s.downloads }()
	file, err := os.Open(filename)
	if err != nil {
		s.finishArchive(task, "failed", &consoleapi.Error{Status: 502, Code: "archive_unavailable", Message: "The archive file is unavailable."})
		writeError(w, 502, "archive_unavailable", "The archive file is unavailable.")
		return
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		s.finishArchive(task, "failed", &consoleapi.Error{Status: 502, Code: "archive_unavailable", Message: "The archive file is unavailable."})
		writeError(w, 502, "archive_unavailable", "The archive file is unavailable.")
		return
	}
	completed := false
	defer func() {
		file.Close()
		if completed {
			s.finishArchive(task, "succeeded", nil)
		} else {
			s.finishArchive(task, "failed", &consoleapi.Error{Status: 502, Code: "archive_download_incomplete", Message: "The archive download did not complete."})
		}
	}()
	completed = s.downloadUsing(w, ctx, "objects.zip", func(context.Context) (consoleapi.Object, error) {
		return consoleapi.Object{Body: file, Size: stat.Size()}, nil
	})
}

func (s *Server) newArchive(w http.ResponseWriter, r *http.Request, sess *session, reader consoleapi.ReferenceBackend) {
	var args struct {
		Refs   []consoleapi.ObjectRef `json:"refs"`
		Bucket string                 `json:"bucket"`
		Prefix string                 `json:"prefix"`
	}
	if !decodeJSON(w, r, &args, 6<<20) {
		return
	}
	if len(args.Refs) > maxPlanKeys || (len(args.Refs) == 0) == (args.Bucket == "") || (len(args.Refs) > 0 && args.Prefix != "") || (args.Bucket != "" && (!validBucket(args.Bucket) || !validKey(args.Prefix))) {
		writeError(w, 400, "invalid_input", "Select up to 1000 objects or one bucket prefix.")
		return
	}
	for _, ref := range args.Refs {
		if !validBucket(ref.Bucket) || ref.Key == "" || !validKey(ref.Key) || !validVersionID(ref.VersionID) {
			writeError(w, 400, "invalid_input", "An archive object or version is invalid.")
			return
		}
	}
	id, err := randomToken()
	if err != nil {
		writeError(w, 500, "internal_error", "Unable to prepare an archive.")
		return
	}
	s.mu.Lock()
	if s.closed || s.rotating || sess.ctx.Err() != nil {
		s.mu.Unlock()
		writeError(w, 409, "connection_ending", "This connection is ending.")
		return
	}
	if len(s.archives) >= 64 {
		s.mu.Unlock()
		writeError(w, 429, "too_many_tasks", "Close or wait for existing archive tasks.")
		return
	}
	owned := 0
	for _, existing := range s.archives {
		if existing.owner == sess {
			owned++
		}
	}
	if owned >= 16 {
		s.mu.Unlock()
		writeError(w, 429, "too_many_tasks", "Wait for existing archive tasks to expire before creating more.")
		return
	}
	if err := tryAcquire(sess.ctx, s.archiveSlots); err != nil {
		s.mu.Unlock()
		writeSlotError(w, err)
		return
	}
	if !sess.runtime.retain() {
		<-s.archiveSlots
		s.mu.Unlock()
		writeError(w, 409, "connection_ending", "This connection is ending.")
		return
	}
	ctx, cancel := context.WithTimeout(sess.ctx, archiveTTL)
	now := time.Now()
	task := &archiveTask{info: archiveReply{ID: id, Status: "planning", Created: now, Expires: now.Add(archiveTTL)}, owner: sess, ctx: ctx, cancel: cancel, slot: true}
	s.archives[id] = task
	task.timer = time.AfterFunc(archiveTTL, func() { s.mu.Lock(); defer s.mu.Unlock(); s.cancelArchiveLocked(task); delete(s.archives, id) })
	s.workers.Add(1)
	initial := task.info
	s.mu.Unlock()
	go func() {
		defer s.workers.Done()
		defer sess.runtime.release()
		s.prepareArchive(task, reader, args.Refs, args.Bucket, args.Prefix)
	}()
	writeJSON(w, 202, initial)
}

func (s *Server) prepareArchive(task *archiveTask, reader consoleapi.ReferenceBackend, refs []consoleapi.ObjectRef, bucket, prefix string) {
	var failure error
	var filename string
	defer func() {
		if failure != nil {
			s.finishArchive(task, "failed", archiveError(failure))
		}
		if task.ctx.Err() != nil {
			s.finishArchive(task, "canceled", nil)
		}
		if filename != "" {
			s.mu.Lock()
			keep := task.path == filename && (task.info.Status == "ready" || task.info.Status == "downloading")
			s.mu.Unlock()
			if !keep {
				_ = os.Remove(filename)
			}
		}
	}()
	planning, cancelPlanning := context.WithTimeout(task.ctx, 60*time.Second)
	if bucket != "" {
		lister, ok := task.owner.runtime.backend.(interface {
			ScanObjects(context.Context, string, string, string, int) (consoleapi.Page, error)
		})
		if !ok {
			cancelPlanning()
			failure = &consoleapi.Error{Status: 501, Code: "archive_prefix_unsupported", Message: "This connection cannot archive prefixes."}
			return
		}
		cursor, seen := "", map[string]bool{}
		for pages := 0; ; pages++ {
			if pages > maxPlanKeys {
				cancelPlanning()
				failure = archiveLimit()
				return
			}
			page, err := lister.ScanObjects(planning, bucket, prefix, cursor, 100)
			if err != nil {
				cancelPlanning()
				failure = err
				return
			}
			for _, entry := range page.Entries {
				if entry.IsPrefix || !strings.HasPrefix(entry.Key, prefix) || entry.Key == "" || !validKey(entry.Key) {
					cancelPlanning()
					failure = &consoleapi.Error{Status: 502, Code: "invalid_archive_listing", Message: "Storage returned an invalid archive listing."}
					return
				}
				refs = append(refs, consoleapi.ObjectRef{Bucket: bucket, Key: entry.Key})
				if len(refs) > maxPlanKeys {
					cancelPlanning()
					failure = archiveLimit()
					return
				}
			}
			if page.NextCursor == "" {
				break
			}
			if seen[page.NextCursor] {
				cancelPlanning()
				failure = &consoleapi.Error{Status: 502, Code: "invalid_archive_listing", Message: "Storage repeated an archive page."}
				return
			}
			seen[page.NextCursor], cursor = true, page.NextCursor
		}
	}
	if len(refs) == 0 {
		cancelPlanning()
		failure = &consoleapi.Error{Status: 400, Code: "empty_archive", Message: "No objects were selected for this archive."}
		return
	}
	items, seenRefs, total := make([]archiveItem, 0, len(refs)), map[consoleapi.ObjectRef]bool{}, int64(0)
	for _, ref := range refs {
		if seenRefs[ref] {
			continue
		}
		seenRefs[ref] = true
		info, err := reader.StatReference(planning, ref)
		if err != nil {
			cancelPlanning()
			failure = err
			return
		}
		if info.Size < 0 || info.Size > s.maxArchiveSize-total {
			cancelPlanning()
			failure = archiveLimit()
			return
		}
		// Current-object selections retain current-object authorization. Their
		// bytes are fixed by If-Match; explicit historical selections keep the
		// requested version and require version-specific authorization upstream.
		if info.ETag == "" && (ref.VersionID == "" || ref.VersionID == "null") {
			cancelPlanning()
			failure = &consoleapi.Error{Status: 409, Code: "archive_target_unstable", Message: "Storage cannot fix the selected object for this archive."}
			return
		}
		total += info.Size
		items = append(items, archiveItem{ObjectRef: ref, Path: fmt.Sprintf("objects/%04d/%s", len(items)+1, archiveLeaf(ref.Key)), Size: info.Size, ETag: info.ETag})
	}
	cancelPlanning()
	if task.ctx.Err() != nil {
		failure = task.ctx.Err()
		return
	}
	file, err := os.CreateTemp(s.archiveDir, "oc-console-archive-*.zip")
	if err != nil {
		failure = errors.New("archive temporary storage unavailable")
		return
	}
	filename = file.Name()
	defer file.Close()
	s.mu.Lock()
	task.path, task.info.Status, task.info.Count, task.info.Size = filename, "running", len(items), total
	task.info.Entries = items
	canceled := task.ctx.Err() != nil
	s.mu.Unlock()
	if canceled {
		failure = task.ctx.Err()
		return
	}
	// Bound archive output as well as source bytes; ZIP64/header overhead cannot
	// turn the source budget into an unbounded temporary-disk allocation.
	output := &archiveOutput{ctx: task.ctx, writer: file, remaining: s.maxArchiveSize + (8 << 20)}
	zw := zip.NewWriter(output)
	buffer := make([]byte, 64<<10)
	for _, item := range items {
		object, err := reader.OpenReference(task.ctx, item.ObjectRef, item.ETag)
		if err != nil {
			if object.Body != nil {
				object.Body.Close()
			}
			failure = err
			return
		}
		if object.Body == nil || object.Size != item.Size {
			if object.Body != nil {
				object.Body.Close()
			}
			failure = &consoleapi.Error{Status: 409, Code: "archive_target_changed", Message: "An archive object changed during preparation."}
			return
		}
		var once sync.Once
		closeObject := func() { once.Do(func() { object.Body.Close() }) }
		stopped := make(chan struct{})
		stop := context.AfterFunc(task.ctx, func() { defer close(stopped); closeObject() })
		entry, err := zw.CreateHeader(&zip.FileHeader{Name: item.Path, Method: zip.Deflate})
		var copied int64
		if err == nil {
			progress := &archiveProgress{server: s, task: task, writer: entry}
			copied, err = io.CopyBuffer(progress, io.LimitReader(readWithIdleTimeout{body: object.Body, idle: s.streamIdle, interrupt: closeObject}, item.Size+1), buffer)
		}
		if !stop() {
			<-stopped
		}
		closeObject()
		if err != nil || copied != item.Size {
			if err == nil {
				err = &consoleapi.Error{Status: 502, Code: "archive_object_incomplete", Message: "An archive object could not be read completely."}
			}
			failure = err
			return
		}
	}
	manifest, err := zw.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Deflate})
	if err == nil {
		err = json.NewEncoder(manifest).Encode(struct {
			Version int           `json:"version"`
			Objects []archiveItem `json:"objects"`
		}{1, items})
	}
	if err == nil {
		err = zw.Close()
	}
	if err == nil {
		err = file.Close()
	}
	if err != nil {
		failure = err
		return
	}
	s.mu.Lock()
	if task.ctx.Err() == nil && task.info.Status == "running" {
		task.info.Status = "ready"
	} else {
		failure = task.ctx.Err()
		if failure == nil {
			failure = context.Canceled
		}
	}
	s.mu.Unlock()
}

type archiveOutput struct {
	ctx       context.Context
	writer    io.Writer
	remaining int64
}

func (w *archiveOutput) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.remaining {
		return 0, archiveLimit()
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}

type archiveProgress struct {
	server *Server
	task   *archiveTask
	writer io.Writer
}

func (w *archiveProgress) Write(p []byte) (int, error) {
	if err := w.task.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(p)
	w.server.mu.Lock()
	w.task.info.Transferred += int64(n)
	w.server.mu.Unlock()
	return n, err
}

func archiveLeaf(key string) string {
	leaf := key[strings.LastIndex(key, "/")+1:]
	leaf = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`\/:<>"|?*`, r) {
			return '_'
		}
		return r
	}, leaf)
	leaf = strings.TrimRight(leaf, ". ")
	for len(leaf) > 180 {
		_, size := utf8.DecodeLastRuneInString(leaf)
		leaf = leaf[:len(leaf)-size]
	}
	if leaf == "" || leaf == "." || leaf == ".." {
		leaf = "object"
	}
	// An ordinal prefix also avoids Windows device names and normalization
	// collisions; manifest retains the unmodified source key.
	return "file-" + leaf
}

func archiveLimit() *consoleapi.Error {
	return &consoleapi.Error{Status: 413, Code: "archive_limit", Message: "The archive exceeds its object or byte limit."}
}
func archiveError(err error) *consoleapi.Error {
	var safe *consoleapi.Error
	if errors.As(err, &safe) && safe != nil {
		copy := *safe
		return &copy
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &consoleapi.Error{Status: 408, Code: "archive_canceled", Message: "Archive preparation was canceled or expired."}
	}
	return &consoleapi.Error{Status: 502, Code: "archive_failed", Message: "Unable to prepare a complete archive."}
}
func (s *Server) releaseArchiveLocked(task *archiveTask) {
	if task.slot {
		<-s.archiveSlots
		task.slot = false
	}
}
func (s *Server) finishArchive(task *archiveTask, status string, err *consoleapi.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task.ctx.Err() != nil || task.info.Status == "canceled" {
		status, err = "canceled", nil
	}
	task.info.Status, task.info.Error = status, err
	if task.path != "" {
		_ = os.Remove(task.path)
		task.path = ""
	}
	s.releaseArchiveLocked(task)
}
func (s *Server) cancelArchiveLocked(task *archiveTask) {
	previous := task.info.Status
	task.info.Status = "canceled"
	task.cancel()
	if previous == "ready" || previous == "failed" || previous == "succeeded" {
		if task.path != "" {
			_ = os.Remove(task.path)
			task.path = ""
		}
		s.releaseArchiveLocked(task)
	}
}
