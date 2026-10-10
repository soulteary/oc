// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

func TestSessionRuntimeRetainsItsStorageConnection(t *testing.T) {
	first := &fakeBackend{list: func(context.Context) ([]consoleapi.Bucket, error) {
		return []consoleapi.Bucket{{Name: "first-user"}}, nil
	}, open: func(context.Context, string, string) (consoleapi.Object, error) {
		return consoleapi.Object{}, &consoleapi.Error{Status: 403, Code: "first-user-denied", Message: "First user denied."}
	}}
	second := &fakeBackend{list: func(context.Context) ([]consoleapi.Bucket, error) {
		return []consoleapi.Bucket{{Name: "second-user"}}, nil
	}}
	s := testServer(t, first, 0)
	firstCookie, firstReply := signIn(t, s)
	// Simulate choosing another authenticated connection for a later login.
	// Existing sessions must never inherit a subsequently selected identity.
	s.backend = second
	secondCookie, _ := signIn(t, s)
	for _, tc := range []struct {
		cookie *http.Cookie
		want   string
	}{{firstCookie, "first-user"}, {secondCookie, "second-user"}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, testRequest(http.MethodGet, "/api/buckets", "", tc.cookie))
		if w.Code != 200 || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("wrong connection: %d %s", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/download?bucket=bucket&key=object", "", firstCookie))
	if w.Code != 403 || !strings.Contains(w.Body.String(), "first-user-denied") {
		t.Fatalf("download used another identity: %d %s", w.Code, w.Body.String())
	}
	logout := testRequest(http.MethodPost, "/api/logout", "", firstCookie)
	logout.Header.Set("X-CSRF-Token", firstReply.CSRFToken)
	s.ServeHTTP(httptest.NewRecorder(), logout)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/buckets", "", secondCookie))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "second-user") {
		t.Fatal("logout revoked another session")
	}
}

func TestSessionRuntimeRetainsItsWriteGate(t *testing.T) {
	s, backend, firstCookie, firstReply := featureServer(t, false, false)
	s.writer = backend
	secondCookie, secondReply := signIn(t, s)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, testRequest(http.MethodGet, "/api/session", "", firstCookie))
	if !strings.Contains(w.Body.String(), `"readOnly":true`) {
		t.Fatal("startup gate changed an existing session")
	}
	if w := jobRequest(s, http.MethodPost, "/api/buckets/create", `{"bucket":"runtime-bucket"}`, firstCookie, firstReply.CSRFToken); w.Code != 403 {
		t.Fatal("original session gained writes")
	}
	if w := jobRequest(s, http.MethodPost, "/api/buckets/create", `{"bucket":"runtime-bucket"}`, secondCookie, secondReply.CSRFToken); w.Code != 200 {
		t.Fatalf("new session lost writes: %s", w.Body.String())
	}
}

func TestDeletionUsesOwningSessionRuntime(t *testing.T) {
	first := &fakeMutationBackend{fakeBackend: &fakeBackend{}}
	second := &fakeMutationBackend{fakeBackend: &fakeBackend{}}
	s := mutationServer(t, first)
	cookie, reply := signIn(t, s)
	planned := jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/plan", `{"bucket":"bucket","keys":["a","b"]}`, cookie, reply.CSRFToken), 200)
	s.backend, s.writer = second, second
	otherCookie, otherReply := signIn(t, s)
	confirm, err := json.Marshal(map[string]string{"confirmToken": planned.ConfirmToken})
	if err != nil {
		t.Fatal(err)
	}
	if w := jobRequest(s, http.MethodPost, "/api/deletions/"+planned.ID+"/execute", string(confirm), otherCookie, otherReply.CSRFToken); w.Code != 404 {
		t.Fatalf("another session accessed deletion plan: %d", w.Code)
	}
	jobResponse(t, jobRequest(s, http.MethodPost, "/api/deletions/"+planned.ID+"/execute", string(confirm), cookie, reply.CSRFToken), 202)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if first.writes.Load() != 2 || second.writes.Load() != 0 {
		t.Fatal("background deletion used another session's connection")
	}
}

func TestPreferencesFollowSessionRuntime(t *testing.T) {
	s := testServer(t, &fakeBackend{}, 0)
	firstCookie, _ := signIn(t, s)
	secondStore, err := newPreferenceStore(t.TempDir(), strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := secondStore.save(userPreferences{Language: "en"}); err != nil {
		t.Fatal(err)
	}
	s.preferences = secondStore
	secondCookie, _ := signIn(t, s)
	for _, tc := range []struct {
		cookie   *http.Cookie
		language string
	}{{firstCookie, "zh"}, {secondCookie, "en"}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, testRequest(http.MethodGet, "/api/preferences", "", tc.cookie))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"language":"`+tc.language+`"`) {
			t.Fatalf("preferences crossed sessions: %d %s", w.Code, w.Body.String())
		}
	}
}
