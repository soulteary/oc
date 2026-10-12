// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

func TestGroupMutationSingleDispatchSignedConditionsAndExactImpact(t *testing.T) {
	for _, scenario := range []string{"confirmed", "conflict", "unknown", "changed-impact", "wrong-state", "missing-capability"} {
		t.Run(scenario, func(t *testing.T) {
			var writes atomic.Int32
			relation := consoleapi.IAMRelationships{Kind: "group", Target: "team", State: "known", CoupledGroups: true, Revision: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(20 * time.Second), AffectedAccessKeys: []string{"member", "child"}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writes.Add(1)
				if r.Method != "PUT" || r.URL.Path != "/otterio/admin/v3/iam-relationship-groups" || r.URL.Query().Get("kind") != "group" || r.URL.Query().Get("target") != "team" {
					t.Error("wrong group protocol")
				}
				if r.Header.Get("X-Otterio-IAM-Impact-If-Match") != relation.Revision {
					t.Error("wrong graph condition")
				}
				for _, header := range []string{"x-otterio-iam-impact-if-match", "x-otterio-iam-impact-expires"} {
					if !strings.Contains(r.Header.Get("Authorization"), header) {
						t.Error("unsigned graph condition")
					}
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 3 || body["action"] != "group.disable" || body["target"] != "team" {
					t.Error("wrong group mutation body")
				}
				switch scenario {
				case "conflict":
					w.WriteHeader(412)
					w.Write([]byte(`{"Code":"PreconditionFailed"}`))
					return
				case "unknown":
					w.WriteHeader(503)
					w.Write([]byte(`{"Code":"IAMPropagationIncomplete"}`))
					return
				}
				impact := relation
				impact.Revision = strings.Repeat("b", 64)
				group := consoleapi.IAMGroup{Name: "team", Status: "disabled", Members: []string{"member"}, Policies: []string{}}
				switch scenario {
				case "changed-impact":
					impact.AffectedAccessKeys = []string{"member"}
				case "wrong-state":
					group.Status = "enabled"
				case "missing-capability":
					impact.CoupledGroups = false
				}
				w.Header().Set("X-Otterio-IAM-Relationships", "v1")
				w.Header().Set("X-Otterio-IAM-Impact-Revision", impact.Revision)
				json.NewEncoder(w).Encode(consoleapi.IAMGroupMutationResult{Outcome: "confirmed", Group: group, Impact: impact})
			}))
			defer server.Close()
			client, err := New(Config{S3URL: server.URL, AdminURL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret, API: "S3v4", Path: "on"})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			relation.StorageTarget = server.URL + "\x00" + server.URL
			result, err := client.IAMMutateRelationshipGroup(context.Background(), consoleapi.IAMActionRequest{Action: "group.disable", Group: "team", ConfirmTarget: "team"}, relation)
			if writes.Load() != 1 {
				t.Fatal("mutation replayed", writes.Load())
			}
			if scenario == "confirmed" {
				if err != nil || result.Outcome != "confirmed" {
					t.Fatal(err)
				}
				return
			}
			var failure *consoleapi.Error
			if !errors.As(err, &failure) {
				t.Fatal("expected typed rejection", err)
			}
			code := "outcome_unknown"
			if scenario == "conflict" {
				code = "identity_revision_conflict"
			}
			if failure.Code != code {
				t.Fatal(failure.Code, code)
			}
		})
	}
}

func TestGroupMutationRefusesUnsupportedOrStaleWithoutDispatch(t *testing.T) {
	var dispatches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { dispatches.Add(1) }))
	defer server.Close()
	client, err := New(Config{S3URL: server.URL, AdminURL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret, API: "S3v4", Path: "on"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	base := consoleapi.IAMRelationships{Kind: "group", Target: "team", State: "known", CoupledGroups: true, Revision: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(20 * time.Second), AffectedAccessKeys: []string{}, StorageTarget: server.URL + "\x00" + server.URL}
	for _, scenario := range []string{"create", "missing-capability", "expired", "wrong-target", "nil-impact"} {
		relation := base
		args := consoleapi.IAMActionRequest{Action: "group.disable", Group: "team", ConfirmTarget: "team"}
		switch scenario {
		case "create":
			args.Action = "group.create"
		case "missing-capability":
			relation.CoupledGroups = false
		case "expired":
			relation.ExpiresAt = time.Now().Add(-time.Second)
		case "wrong-target":
			relation.StorageTarget = "other"
		case "nil-impact":
			relation.AffectedAccessKeys = nil
		}
		if _, err := client.IAMMutateRelationshipGroup(context.Background(), args, relation); err == nil {
			t.Fatal(scenario, "accepted")
		}
	}
	if dispatches.Load() != 0 {
		t.Fatal("untrusted relation dispatched write")
	}
}
