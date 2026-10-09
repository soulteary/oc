// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type fakeIAMBackend struct {
	*fakeMutationBackend
	reads  atomic.Int32
	writes atomic.Int32
	action func(context.Context, consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error)
}

func (b *fakeIAMBackend) IAMUsers(ctx context.Context) (consoleapi.IAMUsers, error) {
	b.reads.Add(1)
	return consoleapi.IAMUsers{Users: []consoleapi.IAMUser{{AccessKey: "alice", Status: "enabled", Policies: []string{"readwrite"}, MemberOf: []string{}}}}, nil
}
func (b *fakeIAMBackend) IAMGroups(context.Context) (consoleapi.IAMGroups, error) {
	b.reads.Add(1)
	return consoleapi.IAMGroups{Groups: []consoleapi.IAMGroup{}}, nil
}
func (b *fakeIAMBackend) IAMServiceAccounts(_ context.Context, user string) (consoleapi.IAMServiceAccounts, error) {
	b.reads.Add(1)
	return consoleapi.IAMServiceAccounts{User: user, ServiceAccounts: []consoleapi.IAMServiceAccount{}}, nil
}
func (b *fakeIAMBackend) IAMPolicies(context.Context) (consoleapi.IAMPolicies, error) {
	b.reads.Add(1)
	return consoleapi.IAMPolicies{Policies: []consoleapi.IAMPolicy{}}, nil
}
func (b *fakeIAMBackend) IAMBindings(_ context.Context, kind, target string) (consoleapi.IAMBindings, error) {
	b.reads.Add(1)
	return consoleapi.IAMBindings{Kind: kind, Target: target, Policies: []string{}, Revision: strings.Repeat("a", 64), Conditional: true}, nil
}
func (b *fakeIAMBackend) IAMAction(ctx context.Context, args consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error) {
	b.writes.Add(1)
	if _, ok := ctx.Deadline(); !ok {
		return consoleapi.IAMActionResult{}, errors.New("missing request deadline")
	}
	if b.action != nil {
		return b.action(ctx, args)
	}
	return consoleapi.IAMActionResult{Outcome: "confirmed"}, nil
}

func iamServer(t *testing.T, writes bool) (*Server, *fakeIAMBackend, *http.Cookie, sessionReply) {
	t.Helper()
	b := &fakeIAMBackend{fakeMutationBackend: &fakeMutationBackend{fakeBackend: &fakeBackend{}}}
	s, err := New(Config{Backend: b, Alias: "local", BaseURL: testOrigin, LoginCode: "test-login-code", AllowWrites: writes})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Wait(ctx); err != nil {
			t.Error(err)
		}
	})
	cookie, reply := signIn(t, s)
	return s, b, cookie, reply
}

func TestIAMReadsAndMutationGuards(t *testing.T) {
	s, b, cookie, reply := iamServer(t, false)
	for _, route := range []string{"/api/iam/users", "/api/iam/groups", "/api/iam/policies", "/api/iam/service-accounts?user=alice"} {
		w := jobRequest(s, http.MethodGet, route, "", cookie, "")
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body.String())
		}
		if w := jobRequest(s, http.MethodGet, route, "", nil, ""); w.Code != 401 {
			t.Fatal("anonymous IAM read accepted")
		}
	}
	for _, route := range []string{"/api/iam/users?user=alice", "/api/iam/service-accounts", "/api/iam/service-accounts?user=alice&user=bob", "/api/iam/service-accounts?user=alice&alias=other"} {
		if w := jobRequest(s, http.MethodGet, route, "", cookie, ""); w.Code != 400 {
			t.Fatalf("accepted %s: %d", route, w.Code)
		}
	}
	body := `{"action":"user.disable","user":"alice","confirmTarget":"alice"}`
	if w := jobRequest(s, http.MethodPost, "/api/iam/actions", body, cookie, reply.CSRFToken); w.Code != 403 {
		t.Fatal("read-only IAM write accepted")
	}
	s.writer = b
	for _, missing := range []string{"cookie", "origin", "csrf"} {
		r := testRequest(http.MethodPost, "/api/iam/actions", body, cookie)
		r.Header.Set("X-CSRF-Token", reply.CSRFToken)
		switch missing {
		case "cookie":
			r.Header.Del("Cookie")
		case "origin":
			r.Header.Del("Origin")
		case "csrf":
			r.Header.Del("X-CSRF-Token")
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("write accepted without %s: %d", missing, w.Code)
		}
	}
	for _, body := range []string{
		`{"action":"user.disable","user":"alice","confirmTarget":"bob"}`,
		`{"action":"user.disable","user":"alice","confirmTarget":"alice","secretKey":"unrelated-secret"}`,
		`{"action":"group.remove-members","group":"team","confirmTarget":"team","members":[]}`,
		`{"action":"service-account.rotate","accessKey":"service","confirmTarget":"service"}`,
		`{"action":"service-account.create","user":"alice","secretKey":"supplied-service-secret","confirmTarget":"alice"}`,
		`{"action":"user.delete","user":"alice","confirmTarget":"alice","alias":"other"}`,
	} {
		if w := jobRequest(s, http.MethodPost, "/api/iam/actions", body, cookie, reply.CSRFToken); w.Code != 400 {
			t.Fatalf("accepted invalid action: %d %s", w.Code, w.Body.String())
		}
	}
	if b.reads.Load() != 4 || b.writes.Load() != 0 {
		t.Fatal("rejected request reached IAM backend")
	}
}

type iamAcknowledgementWriter struct {
	*httptest.ResponseRecorder
	onWrite func()
}

func (w *iamAcknowledgementWriter) Write(body []byte) (int, error) {
	w.onWrite()
	return w.ResponseRecorder.Write(body)
}

func TestIAMResumesReadsBeforeAcknowledgement(t *testing.T) {
	for _, denied := range []bool{false, true} {
		s, b, cookie, reply := iamServer(t, true)
		if denied {
			b.action = func(context.Context, consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error) {
				return consoleapi.IAMActionResult{}, &consoleapi.Error{Status: 403, Code: "AccessDenied", Message: "Denied."}
			}
		}
		w := &iamAcknowledgementWriter{ResponseRecorder: httptest.NewRecorder(), onWrite: func() {
			read := jobRequest(s, http.MethodGet, "/api/iam/service-accounts?user=alice", "", cookie, "")
			if read.Code != http.StatusOK {
				t.Fatalf("read during acknowledgement: %d %s", read.Code, read.Body.String())
			}
		}}
		r := testRequest(http.MethodPost, "/api/iam/actions", `{"action":"user.disable","user":"alice","confirmTarget":"alice"}`, cookie)
		r.Header.Set("X-CSRF-Token", reply.CSRFToken)
		s.ServeHTTP(w, r)
		if len(s.writeSlots) != 0 {
			t.Fatal("write slots leaked")
		}
	}
}

func TestIAMRetiresOnlyAfterSuccessOrUnknownAndReservesAllWrites(t *testing.T) {
	for _, test := range []struct {
		name    string
		restart bool
		err     error
		closed  bool
		status  int
	}{
		{"other-account", false, nil, false, 200},
		{"current-account", true, nil, true, 200},
		{"known-denial", true, &consoleapi.Error{Status: 403, Code: "AccessDenied", Message: "Denied."}, false, 403},
		{"unknown", false, &consoleapi.Error{Status: 502, Code: "outcome_unknown", Message: "Unknown."}, true, 502},
		{"untyped", false, errors.New("upstream secret must not leak"), true, 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, b, cookie, reply := iamServer(t, true)
			b.action = func(_ context.Context, args consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error) {
				if len(s.writeSlots) != cap(s.writeSlots) || !s.rotating {
					t.Error("IAM did not reserve all write slots")
				}
				return consoleapi.IAMActionResult{Outcome: "confirmed", RestartRequired: test.restart}, test.err
			}
			w := jobRequest(s, http.MethodPost, "/api/iam/actions", `{"action":"user.disable","user":"alice","confirmTarget":"alice"}`, cookie, reply.CSRFToken)
			if w.Code != test.status || strings.Contains(w.Body.String(), "secret must not leak") {
				t.Fatalf("unsafe result: %d %s", w.Code, w.Body.String())
			}
			select {
			case <-s.Done():
				if !test.closed {
					t.Fatal("known rejection retired console")
				}
			default:
				if test.closed {
					t.Fatal("invalidated identity retained")
				}
			}
			if len(s.writeSlots) != 0 {
				t.Fatal("write slots leaked")
			}
		})
	}
	s, b, cookie, reply := iamServer(t, true)
	s.writeSlots <- struct{}{}
	w := jobRequest(s, http.MethodPost, "/api/iam/actions", `{"action":"user.delete","user":"alice","confirmTarget":"alice"}`, cookie, reply.CSRFToken)
	<-s.writeSlots
	if w.Code != 409 || b.writes.Load() != 0 {
		t.Fatal("IAM invalidated credentials during active storage write")
	}
}
