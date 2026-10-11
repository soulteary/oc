// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

func TestIAMRelationshipsKnownEmptyAndMalformed(t *testing.T) {
	now := time.Now().UTC()
	revision := strings.Repeat("a", 64)
	header := http.Header{}
	header.Set("X-Otterio-IAM-Relationships", "v1")
	header.Set("X-Otterio-IAM-Impact-Revision", revision)
	result := consoleapi.IAMRelationships{Kind: "group", Target: "empty-group", State: "known", Revision: revision, ExpiresAt: now.Add(30 * time.Second), AffectedAccessKeys: []string{}}
	encode := func(value any) []byte {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	parsed, err := decodeIAMRelationships(encode(result), header, "group", "empty-group", now)
	if err != nil || parsed.AffectedAccessKeys == nil || len(parsed.AffectedAccessKeys) != 0 {
		t.Fatal("known empty lost")
	}
	for _, mutation := range []func(*consoleapi.IAMRelationships){
		func(r *consoleapi.IAMRelationships) { r.State = "unknown" }, func(r *consoleapi.IAMRelationships) { r.Revision = "" }, func(r *consoleapi.IAMRelationships) { r.Target = "other" }, func(r *consoleapi.IAMRelationships) { r.AffectedAccessKeys = nil }, func(r *consoleapi.IAMRelationships) { r.ExpiresAt = now }, func(r *consoleapi.IAMRelationships) { r.ExpiresAt = now.Add(2 * time.Minute) }, func(r *consoleapi.IAMRelationships) { r.AffectedAccessKeys = []string{"same-key", "same-key"} }, func(r *consoleapi.IAMRelationships) { r.AffectedAccessKeys = []string{"bad/key"} },
	} {
		value := result
		mutation(&value)
		if _, err := decodeIAMRelationships(encode(value), header, "group", "empty-group", now); err == nil {
			t.Fatal("unknown/malformed snapshot accepted")
		}
	}
	var fields map[string]json.RawMessage
	json.Unmarshal(encode(result), &fields)
	delete(fields, "affectedAccessKeys")
	if _, err := decodeIAMRelationships(encode(fields), header, "group", "empty-group", now); err == nil {
		t.Fatal("missing set became known empty")
	}
	duplicate := append([]byte(`{"state":"unknown",`), encode(result)[1:]...)
	if _, err := decodeIAMRelationships(duplicate, header, "group", "empty-group", now); err == nil {
		t.Fatal("duplicate JSON field accepted")
	}
	header.Add("X-Otterio-IAM-Impact-Revision", revision)
	if _, err := decodeIAMRelationships(encode(result), header, "group", "empty-group", now); err == nil {
		t.Fatal("ambiguous revision header accepted")
	}
}
