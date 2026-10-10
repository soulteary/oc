package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPreferencesPersistAndSeparateIdentities(t *testing.T) {
	dir := t.TempDir()
	identity := strings.Repeat("a", 64)
	p, err := newPreferenceStore(dir, identity)
	if err != nil {
		t.Fatal(err)
	}
	value := userPreferences{Language: "en", Favorites: []favoriteReference{{"bucket", "a +/中文.jpg"}}, Recent: []string{"bucket"}}
	if err = p.save(value); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, identity+".json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private file: %v %v", info, err)
	}
	loaded, err := newPreferenceStore(dir, identity)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.value.Language != "en" || loaded.value.Favorites[0].Key != value.Favorites[0].Key || loaded.value.Recent[0] != "bucket" {
		t.Fatal("preferences did not survive restart")
	}
	other, err := newPreferenceStore(dir, strings.Repeat("b", 64))
	if err != nil || len(other.value.Favorites) != 0 || len(other.value.Recent) != 0 {
		t.Fatal("identities shared preferences")
	}
}

func TestPreferencesAuthenticateAndRequireCSRF(t *testing.T) {
	s, _ := archiveServer(t, &archiveBackend{}, 1024)
	cookie, session := signIn(t, s)
	if w := preferenceRequest(s, "GET", "/api/preferences", "", nil, ""); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	body := `{"action":"favorite-add","bucket":"bucket","key":"a +/中文.jpg"}`
	if w := preferenceRequest(s, "PUT", "/api/preferences", body, cookie, ""); w.Code != 403 {
		t.Fatalf("csrf: %d", w.Code)
	}
	for _, op := range []string{body, body, `{"action":"visit","bucket":"bucket"}`, `{"action":"language","language":"en"}`} {
		if w := preferenceRequest(s, "PUT", "/api/preferences", op, cookie, session.CSRFToken); w.Code != 200 {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
	}
	w := preferenceRequest(s, "GET", "/api/preferences", "", cookie, "")
	var value userPreferences
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if len(value.Favorites) != 1 || len(value.Recent) != 1 || value.Language != "en" {
		t.Fatalf("state: %#v", value)
	}
	for _, body := range []string{`{"action":"language","language":"unknown"}`, `{"action":"visit","bucket":""}`, `{"action":"favorite-add","bucket":"bucket","key":""}`, `{"action":"visit","bucket":"bucket","unexpected":true}`} {
		if w := preferenceRequest(s, "PUT", "/api/preferences", body, cookie, session.CSRFToken); w.Code == 200 {
			t.Fatal("invalid preference accepted")
		}
	}
	if w := preferenceRequest(s, "PUT", "/api/preferences", `{"action":"favorite-remove","bucket":"bucket","key":"a +/中文.jpg"}`, cookie, session.CSRFToken); w.Code != 200 {
		t.Fatal("remove failed")
	}
	if len(s.preferences.value.Favorites) != 0 {
		t.Fatal("favorite still present")
	}
}

func TestPreferenceWriteFailureKeepsPreviousState(t *testing.T) {
	dir := t.TempDir()
	p, err := newPreferenceStore(dir, strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err = p.save(userPreferences{Language: "en"}); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if p.value.Language != "zh" {
		t.Fatal("failed write committed memory state")
	}
}

func TestPreferenceUpdatesFromConcurrentSessionsAreNotLost(t *testing.T) {
	s, _ := archiveServer(t, &archiveBackend{}, 1024)
	s.writer = nil // User preferences are writable even when storage writes are off.
	dir := t.TempDir()
	identity := strings.Repeat("d", 64)
	var err error
	s.preferences, err = newPreferenceStore(dir, identity)
	if err != nil {
		t.Fatal(err)
	}
	cookie, session := signIn(t, s)
	other, otherSession := signIn(t, s)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, csrf := cookie, session.CSRFToken
			if i%2 == 1 {
				c, csrf = other, otherSession.CSRFToken
			}
			body := fmt.Sprintf(`{"action":"favorite-add","bucket":"bucket","key":"key-%d"}`, i)
			if w := preferenceRequest(s, "PUT", "/api/preferences", body, c, csrf); w.Code != 200 {
				t.Errorf("concurrent save: %d", w.Code)
			}
		}(i)
	}
	wg.Wait()
	loaded, err := newPreferenceStore(dir, identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.value.Favorites) != 12 {
		t.Fatalf("lost updates: %d", len(loaded.value.Favorites))
	}
}

func preferenceRequest(s *Server, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := testRequest(method, path, body, cookie)
	if method != "GET" {
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
