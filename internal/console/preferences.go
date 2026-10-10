package console

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type favoriteReference struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}
type userPreferences struct {
	Language  string              `json:"language"`
	Favorites []favoriteReference `json:"favorites"`
	Recent    []string            `json:"recent"`
}
type preferenceStore struct {
	mu    sync.Mutex
	path  string
	value userPreferences
}

// Retired sessions can still have requests finishing a preference write. Keep
// one store for the principal until its last client's request leases drain.
type principalPreferences struct {
	store *preferenceStore
	refs  int
}

func (s *Server) releasePrincipalPreferences(identity string, entry *principalPreferences) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.refs--
	if entry.refs == 0 && s.userPreferences[identity] == entry {
		delete(s.userPreferences, identity)
	}
}

func newPreferenceStore(dir, identity string) (*preferenceStore, error) {
	p := &preferenceStore{value: userPreferences{Language: "zh", Favorites: []favoriteReference{}, Recent: []string{}}}
	if dir == "" {
		return p, nil
	}
	_, identityError := hex.DecodeString(identity)
	if len(identity) != 64 || identityError != nil {
		return nil, errors.New("preferences require a hashed storage identity")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	p.path = filepath.Join(dir, identity+".json")
	data, err := os.ReadFile(p.path)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 || json.Unmarshal(data, &p.value) != nil {
		return nil, errors.New("invalid user preferences file")
	}
	if err := validatePreferences(p.value); err != nil {
		return nil, err
	}
	return p, nil
}
func validatePreferences(p userPreferences) error {
	if p.Language != "zh" && p.Language != "en" {
		return errors.New("invalid language")
	}
	if len(p.Favorites) > 1000 || len(p.Recent) > 20 {
		return errors.New("too many preferences")
	}
	for _, f := range p.Favorites {
		if f.Bucket == "" || len(f.Bucket) > 255 || f.Key == "" || len(f.Key) > 4096 {
			return errors.New("invalid favorite")
		}
	}
	for _, b := range p.Recent {
		if b == "" || len(b) > 255 {
			return errors.New("invalid recent bucket")
		}
	}
	return nil
}
func (p *preferenceStore) save(v userPreferences) error {
	if err := validatePreferences(v); err != nil {
		return err
	}
	if p.path != "" {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		f, err := os.CreateTemp(filepath.Dir(p.path), ".preferences-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err = f.Write(data); err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err = os.Rename(f.Name(), p.path); err != nil {
			return err
		}
	}
	p.value = v
	return nil
}
func (s *Server) servePreferences(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		s.requireMethod(w, r, http.MethodGet)
		return
	}
	sess, _ := s.authenticate(r)
	if sess == nil {
		writeError(w, 401, "login_required", s.loginPrompt())
		return
	}
	p := sess.runtime.preferences
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.Method == http.MethodPut {
		if !s.requireOrigin(w, r) || !s.requireCSRF(w, r, sess) {
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeError(w, 400, "invalid_preferences", "Expected application/json.")
			return
		}
		var op struct {
			Action   string `json:"action"`
			Language string `json:"language"`
			Bucket   string `json:"bucket"`
			Key      string `json:"key"`
		}
		d := json.NewDecoder(io.LimitReader(r.Body, 8193))
		d.DisallowUnknownFields()
		if d.Decode(&op) != nil || d.Decode(&struct{}{}) != io.EOF {
			writeError(w, 400, "invalid_preferences", "Invalid preferences request.")
			return
		}
		v := p.value
		v.Favorites = append([]favoriteReference{}, v.Favorites...)
		v.Recent = append([]string{}, v.Recent...)
		switch op.Action {
		case "language":
			v.Language = op.Language
		case "favorite-add", "favorite-remove":
			if op.Bucket == "" || len(op.Bucket) > 255 || op.Key == "" || len(op.Key) > 4096 {
				writeError(w, 400, "invalid_preferences", "Invalid favorite.")
				return
			}
			found := false
			out := []favoriteReference{}
			for _, f := range v.Favorites {
				if f.Bucket == op.Bucket && f.Key == op.Key {
					found = true
					if op.Action == "favorite-remove" {
						continue
					}
				}
				out = append(out, f)
			}
			if !found && op.Action == "favorite-add" {
				out = append(out, favoriteReference{op.Bucket, op.Key})
			}
			v.Favorites = out
		case "visit":
			if op.Bucket == "" || len(op.Bucket) > 255 {
				writeError(w, 400, "invalid_preferences", "Invalid bucket.")
				return
			}
			out := []string{op.Bucket}
			for _, b := range v.Recent {
				if b != op.Bucket && len(out) < 20 {
					out = append(out, b)
				}
			}
			v.Recent = out
		default:
			writeError(w, 400, "invalid_preferences", "Unknown preferences action.")
			return
		}
		if err := validatePreferences(v); err != nil {
			writeError(w, 400, "invalid_preferences", "Invalid preferences values.")
			return
		}
		if err := p.save(v); err != nil {
			writeError(w, 500, "preferences_save_failed", "Unable to save preferences.")
			return
		}
	}
	writeJSON(w, 200, p.value)
}
