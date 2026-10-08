// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

type fakeSettingsBackend struct {
	*fakeMutationBackend
	reads   atomic.Int32
	saves   atomic.Int32
	rotates atomic.Int32
	setting func(context.Context, string, string) (consoleapi.BucketSetting, error)
	save    func(context.Context, string, string, string, string, bool) (consoleapi.BucketSetting, error)
	rotate  func(context.Context, string) error
}

func (b *fakeSettingsBackend) BucketSetting(ctx context.Context, bucket, kind string) (consoleapi.BucketSetting, error) {
	b.reads.Add(1)
	if b.setting != nil {
		return b.setting(ctx, bucket, kind)
	}
	return consoleapi.BucketSetting{Bucket: bucket, Kind: kind, Format: "json", Document: `{}`, Revision: strings.Repeat("a", 64), Conditional: true}, nil
}
func (b *fakeSettingsBackend) SaveBucketSetting(ctx context.Context, bucket, kind, doc, revision string, remove bool) (consoleapi.BucketSetting, error) {
	b.saves.Add(1)
	if b.save != nil {
		return b.save(ctx, bucket, kind, doc, revision, remove)
	}
	return consoleapi.BucketSetting{Bucket: bucket, Kind: kind, Document: doc, Revision: strings.Repeat("b", 64), Conditional: true}, nil
}
func (b *fakeSettingsBackend) SelfAccount(context.Context) (consoleapi.SelfAccount, error) {
	b.reads.Add(1)
	return consoleapi.SelfAccount{Kind: "iam", Status: "enabled", CanRotateSecret: true}, nil
}
func (b *fakeSettingsBackend) RotateOwnSecret(ctx context.Context, secret string) error {
	b.rotates.Add(1)
	if b.rotate != nil {
		return b.rotate(ctx, secret)
	}
	return nil
}

func settingsServer(t *testing.T, writes bool) (*Server, *fakeSettingsBackend, *http.Cookie, sessionReply) {
	t.Helper()
	b := &fakeSettingsBackend{fakeMutationBackend: &fakeMutationBackend{fakeBackend: &fakeBackend{}}}
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

func settingsBody(value any) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func TestSettingsReadsAreIndependentOfBucketEnumerationAndWrites(t *testing.T) {
	s, b, cookie, _ := settingsServer(t, false)
	b.fakeBackend.list = func(context.Context) ([]consoleapi.Bucket, error) {
		t.Fatal("settings enumerated buckets")
		return nil, nil
	}
	for _, route := range []string{"/api/bucket-settings?bucket=exact-bucket&kind=policy", "/api/self-account"} {
		w := jobRequest(s, http.MethodGet, route, "", cookie, "")
		if w.Code != 200 {
			t.Fatalf("%s => %d: %s", route, w.Code, w.Body.String())
		}
	}
	for _, query := range []string{"", "?bucket=exact-bucket", "?bucket=exact-bucket&kind=policy&kind=versioning", "?bucket=exact-bucket&kind=policy&alias=other", "?bucket=x/y&kind=policy", "?bucket=exact-bucket&kind=other"} {
		w := jobRequest(s, http.MethodGet, "/api/bucket-settings"+query, "", cookie, "")
		if w.Code != 400 {
			t.Fatalf("accepted %q: %d", query, w.Code)
		}
	}
	if b.reads.Load() != 2 {
		t.Fatal("invalid reads reached backend")
	}
}

func TestSettingsWritesHaveOriginCSRFConfirmationAndReadOnlyGates(t *testing.T) {
	valid := `{"bucket":"exact-bucket","kind":"policy","document":"{}","revision":"` + strings.Repeat("a", 64) + `","confirm":true}`
	for _, route := range []string{"/api/bucket-settings", "/api/self-secret"} {
		body := valid
		if route == "/api/self-secret" {
			body = `{"newSecret":"new-secret-value","confirm":true}`
		}
		t.Run(route, func(t *testing.T) {
			s, b, cookie, reply := settingsServer(t, false)
			if w := jobRequest(s, http.MethodPost, route, body, cookie, reply.CSRFToken); w.Code != 403 {
				t.Fatal("read-only accepted mutation")
			}
			s.writer = b
			for _, which := range []string{"cookie", "origin", "csrf"} {
				r := testRequest(http.MethodPost, route, body, cookie)
				r.Header.Set("X-CSRF-Token", reply.CSRFToken)
				switch which {
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
					t.Fatalf("accepted without %s: %d", which, w.Code)
				}
			}
			for _, bad := range []string{strings.ReplaceAll(body, `"confirm":true`, `"confirm":false`), strings.TrimSuffix(body, "}") + `,"accessKey":"other"}`, body + "{}"} {
				if w := jobRequest(s, http.MethodPost, route, bad, cookie, reply.CSRFToken); w.Code != 400 {
					t.Fatalf("accepted invalid body: %d", w.Code)
				}
			}
			if b.saves.Load()+b.rotates.Load() != 0 {
				t.Fatal("rejected request reached backend")
			}
		})
	}
}

func TestSettingReplacementPreservesDocumentAndRevision(t *testing.T) {
	s, b, cookie, reply := settingsServer(t, true)
	doc := `<LifecycleConfiguration><Rule><ID>keep-extension</ID><Unknown>完整</Unknown></Rule></LifecycleConfiguration>`
	b.save = func(_ context.Context, bucket, kind, gotDoc, rev string, remove bool) (consoleapi.BucketSetting, error) {
		if bucket != "exact-bucket" || kind != "lifecycle" || gotDoc != doc || rev != strings.Repeat("a", 64) || remove {
			t.Fatal("replacement target, revision or raw document changed")
		}
		return consoleapi.BucketSetting{}, &consoleapi.Error{Status: 412, Code: "setting_conflict", Message: "Reload the current setting."}
	}
	w := jobRequest(s, http.MethodPost, "/api/bucket-settings", settingsBody(map[string]any{"bucket": "exact-bucket", "kind": "lifecycle", "document": doc, "revision": strings.Repeat("a", 64), "confirm": true}), cookie, reply.CSRFToken)
	if w.Code != 412 || b.saves.Load() != 1 {
		t.Fatalf("conflict not preserved: %d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{
		`{"bucket":"exact-bucket","kind":"policy","document":"{}","revision":"bad","confirm":true}`,
		`{"bucket":"exact-bucket","kind":"versioning","remove":true,"revision":"` + strings.Repeat("a", 64) + `","confirm":true}`,
	} {
		if w := jobRequest(s, http.MethodPost, "/api/bucket-settings", body, cookie, reply.CSRFToken); w.Code != 400 {
			t.Fatal("invalid replacement reached backend")
		}
	}
}

func TestSecretRotationQuiescesWritesAndRetiresAllSessionsAfterAcknowledgement(t *testing.T) {
	for _, outcome := range []string{"success", "unknown", "raw-error", "denied", "unsupported", "discovery-failed"} {
		t.Run(outcome, func(t *testing.T) {
			s, b, cookie, reply := settingsServer(t, true)
			otherCookie, _ := signIn(t, s)
			other, _ := s.authenticate(testRequest(http.MethodGet, "/api/session", "", otherCookie))
			entered, release := make(chan struct{}), make(chan struct{})
			b.rotate = func(ctx context.Context, secret string) error {
				if secret != "new-secret-value" || len(s.writeSlots) != cap(s.writeSlots) {
					t.Error("rotation did not reserve all writes")
				}
				close(entered)
				<-release
				switch outcome {
				case "unknown":
					return &consoleapi.Error{Status: 502, Code: "outcome_unknown", Message: "uncertain"}
				case "raw-error":
					return errors.New("private upstream error")
				case "denied":
					return &consoleapi.Error{Status: 403, Code: "access_denied", Message: "Denied."}
				case "unsupported":
					return &consoleapi.Error{Status: 501, Code: "secret_rotation_unsupported", Message: "Unsupported."}
				case "discovery-failed":
					return &consoleapi.Error{Status: 502, Code: "UpstreamError", Message: "Discovery failed."}
				}
				return nil
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- jobRequest(s, http.MethodPost, "/api/self-secret", `{"newSecret":"new-secret-value","confirm":true}`, cookie, reply.CSRFToken)
			}()
			waitSignal(t, entered)
			if w := jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"exact-bucket","key":"object","size":0}`, cookie, reply.CSRFToken); w.Code != 503 {
				t.Fatalf("accepted write during rotation: %d", w.Code)
			}
			close(release)
			w := <-done
			if strings.Contains(w.Body.String(), "new-secret-value") || strings.Contains(w.Body.String(), "private upstream error") {
				t.Fatal("rotation leaked private values")
			}
			if outcome == "denied" || outcome == "unsupported" || outcome == "discovery-failed" {
				if w.Code < 400 || other.ctx.Err() != nil || s.life.Err() != nil || !strings.Contains(w.Body.String(), `"restartRequired":false`) {
					t.Fatal("known rejection retired valid identity")
				}
				return
			}
			if !w.Flushed || !strings.Contains(w.Body.String(), `"restartRequired":true`) {
				t.Fatal("did not flush restart acknowledgement")
			}
			waitSignal(t, s.Done())
			waitSignal(t, other.ctx.Done())
			if w := jobRequest(s, http.MethodPost, "/api/login", `{"code":"test-login-code"}`, nil, ""); w.Code != 503 {
				t.Fatal("old login code reopened retired identity")
			}
		})
	}
}

func TestSecretRotationRejectsBusyWritesAndByteInvalidSecrets(t *testing.T) {
	s, b, cookie, reply := settingsServer(t, true)
	s.writeSlots <- struct{}{}
	w := jobRequest(s, http.MethodPost, "/api/self-secret", `{"newSecret":"new-secret-value","confirm":true}`, cookie, reply.CSRFToken)
	if w.Code != 409 || b.rotates.Load() != 0 || len(s.writeSlots) != 1 {
		t.Fatal("rotation raced active write or leaked slot")
	}
	<-s.writeSlots
	for _, secret := range []string{"short", strings.Repeat("密", 43), "secret-with\nnewline", "secret-with\x00null"} {
		w := jobRequest(s, http.MethodPost, "/api/self-secret", settingsBody(map[string]any{"newSecret": secret, "confirm": true}), cookie, reply.CSRFToken)
		if w.Code != 400 || b.rotates.Load() != 0 {
			t.Fatal("invalid secret accepted")
		}
	}
}

// Logout remains a local security action while the selected identity is paused.
// It must cancel only the logging-out session and must not permit new writes.
func TestSecretRotationStillAllowsAuthenticatedLogout(t *testing.T) {
	for _, owner := range []bool{true, false} {
		t.Run(map[bool]string{true: "rotation-owner", false: "other-session"}[owner], func(t *testing.T) {
			s, b, cookie, reply := settingsServer(t, true)
			otherCookie, otherReply := signIn(t, s)
			other, _ := s.authenticate(testRequest(http.MethodGet, "/api/session", "", otherCookie))
			entered, release := make(chan struct{}), make(chan struct{})
			canceled := make(chan struct{}, 1)
			b.rotate = func(ctx context.Context, _ string) error {
				close(entered)
				select {
				case <-ctx.Done():
					canceled <- struct{}{}
				case <-release:
				}
				// This fake represents a preflight which has not dispatched PUT.
				return &consoleapi.Error{Status: 403, Code: "AccessDenied", Message: "Denied before writing."}
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- jobRequest(s, http.MethodPost, "/api/self-secret", `{"newSecret":"new-secret-value","confirm":true}`, cookie, reply.CSRFToken)
			}()
			waitSignal(t, entered)
			logoutCookie, logoutReply := cookie, reply
			if !owner {
				logoutCookie, logoutReply = otherCookie, otherReply
			}
			if w := jobRequest(s, http.MethodPost, "/api/logout", "", logoutCookie, "invalid-csrf"); w.Code != 403 {
				t.Fatalf("logout did not enforce CSRF while rotating: %d", w.Code)
			}
			w := jobRequest(s, http.MethodPost, "/api/logout", "", logoutCookie, logoutReply.CSRFToken)
			if w.Code != 204 {
				t.Fatalf("logout was blocked by credential rotation: %d", w.Code)
			}
			if sess, _ := s.authenticate(testRequest(http.MethodGet, "/api/session", "", logoutCookie)); sess != nil {
				t.Fatal("logout retained a usable session")
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].MaxAge != -1 {
				t.Fatal("logout did not expire the browser cookie")
			}
			if owner {
				waitSignal(t, canceled)
				if other.ctx.Err() != nil {
					t.Fatal("owner logout canceled an unrelated session before a result")
				}
			} else {
				waitSignal(t, other.ctx.Done())
				select {
				case <-canceled:
					t.Fatal("unrelated logout canceled the rotating request")
				default:
				}
				if w := jobRequest(s, http.MethodPost, "/api/uploads", `{"bucket":"exact-bucket","key":"blocked","size":0}`, cookie, reply.CSRFToken); w.Code != 503 {
					t.Fatal("logout accidentally unpaused writes")
				}
				close(release)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("rotating request did not finish")
			}
			if s.life.Err() != nil || len(s.writeSlots) != 0 {
				t.Fatal("known preflight failure retired the process or leaked slots")
			}
		})
	}
}

func TestLoginStartedBeforeRotationCannotRegisterSessionWhilePaused(t *testing.T) {
	s, b, cookie, reply := settingsServer(t, true)
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	b.rotate = func(context.Context, string) error {
		close(entered)
		<-release
		return &consoleapi.Error{Status: 403, Code: "AccessDenied", Message: "Denied before writing."}
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	request := testRequest(http.MethodPost, "/api/login", "", nil)
	request.Body = reader
	const loginBody = `{"code":"test-login-code"}`
	request.ContentLength = int64(len(loginBody))
	loginDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		s.ServeHTTP(response, request)
		loginDone <- response
	}()
	// A consumed byte proves the login passed ServeHTTP's rotation check,
	// while the rest of the body keeps it from registering its session.
	if _, err := writer.Write([]byte(loginBody[:1])); err != nil {
		t.Fatal(err)
	}
	rotationDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rotationDone <- jobRequest(s, http.MethodPost, "/api/self-secret", `{"newSecret":"new-secret-value","confirm":true}`, cookie, reply.CSRFToken)
	}()
	waitSignal(t, entered)
	if _, err := writer.Write([]byte(loginBody[1:])); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	select {
	case response := <-loginDone:
		if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"credential_change_pending"`) || len(response.Result().Cookies()) != 0 {
			t.Fatalf("in-flight login registered during rotation: status=%d cookies=%d", response.Code, len(response.Result().Cookies()))
		}
	case <-time.After(time.Second):
		t.Fatal("completed login body did not return")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) != 1 || !s.rotating || len(s.writeSlots) != cap(s.writeSlots) {
		t.Fatal("rejected login changed sessions or released the rotation slots")
	}
}

func TestSettingsRejectMalformedUTF8BeforeJSONCanReplaceIt(t *testing.T) {
	for _, kind := range []string{"secret", "policy"} {
		t.Run(kind, func(t *testing.T) {
			s, b, cookie, reply := settingsServer(t, true)
			route := "/api/self-secret"
			body := "{\"newSecret\":\"abcdefg" + string([]byte{0xff}) + "\",\"confirm\":true}"
			if kind == "policy" {
				route = "/api/bucket-settings"
				body = "{\"bucket\":\"exact-bucket\",\"kind\":\"policy\",\"document\":\"{\\\"name\\\":\\\"" + string([]byte{0xff}) + "\\\"}\",\"revision\":\"" + strings.Repeat("a", 64) + "\",\"confirm\":true}"
			}
			w := jobRequest(s, http.MethodPost, route, body, cookie, reply.CSRFToken)
			if w.Code != 400 || b.saves.Load()+b.rotates.Load() != 0 {
				t.Fatalf("malformed UTF-8 was replaced and dispatched: status=%d saves=%d rotates=%d", w.Code, b.saves.Load(), b.rotates.Load())
			}
			if s.life.Err() != nil {
				t.Fatal("invalid input retired the connection")
			}
		})
	}
}

func TestSettingsRejectUnpairedUnicodeEscapesWithoutChangingSecrets(t *testing.T) {
	for _, secret := range []string{`abcdefg\ud800`, `abcdefg\udc00`, `abcdefg\ud800x`, `abcdefg\ud800\ud800`} {
		s, b, cookie, reply := settingsServer(t, true)
		w := jobRequest(s, http.MethodPost, "/api/self-secret", `{"newSecret":"`+secret+`","confirm":true}`, cookie, reply.CSRFToken)
		if w.Code != 400 || b.rotates.Load() != 0 || s.life.Err() != nil {
			t.Fatalf("unpaired Unicode escape reached secret rotation: status=%d rotates=%d", w.Code, b.rotates.Load())
		}
	}
	for _, secret := range []string{`abcdefg\ud83d\ude00`, `abcdefg\uFFFD`, `abcdefg\\ud800`} {
		s, b, cookie, reply := settingsServer(t, true)
		w := jobRequest(s, http.MethodPost, "/api/self-secret", `{"newSecret":"`+secret+`","confirm":true}`, cookie, reply.CSRFToken)
		if w.Code != 200 || b.rotates.Load() != 1 {
			t.Fatal("valid Unicode pair, replacement character or literal escape was rejected")
		}
	}
}
