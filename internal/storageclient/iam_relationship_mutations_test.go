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

func TestIAMRelationshipMutationSingleDispatchSignedConditions(t *testing.T) {
	for _, scenario := range []string{"confirmed", "conflict", "unknown", "changed-impact", "changed-policy"} {
		t.Run(scenario, func(t *testing.T) {
			var writes atomic.Int32
			revision := strings.Repeat("a", 64)
			relation := consoleapi.IAMRelationships{Kind: "user", Target: "native-parent", State: "known", CoupledBindings: true, Revision: revision, ExpiresAt: time.Now().UTC().Add(30 * time.Second), AffectedAccessKeys: []string{"native-parent", "native-child"}}
			binding := consoleapi.IAMBindings{Kind: "user", Target: "native-parent", Conditional: true, Revision: revision, Policies: []string{}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.Header().Set("X-Otterio-IAM-Relationships", "v1")
					w.Header().Set("X-Otterio-IAM-Impact-Revision", revision)
					json.NewEncoder(w).Encode(relation)
					return
				}
				writes.Add(1)
				if r.URL.Path != "/otterio/admin/v3/iam-relationship-bindings" || r.Header.Get("X-Otterio-IAM-Impact-If-Match") != revision || r.Header.Get(iamBindingsCondition) != revision {
					t.Error("wrong coupled protocol")
				}
				authorization := r.Header.Get("Authorization")
				for _, name := range []string{"x-otterio-iam-impact-if-match", "x-otterio-iam-impact-expires", strings.ToLower(iamBindingsCondition)} {
					if !strings.Contains(authorization, name) {
						t.Error("unsigned mutation condition", name)
					}
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
				after := relation
				after.Revision = strings.Repeat("b", 64)
				after.ExpiresAt = time.Now().UTC().Add(30 * time.Second)
				afterBinding := binding
				afterBinding.Revision = strings.Repeat("c", 64)
				afterBinding.Policies = []string{"readonly"}
				if scenario == "changed-impact" {
					after.AffectedAccessKeys = []string{"native-parent"}
				}
				if scenario == "changed-policy" {
					afterBinding.Policies = []string{"admin-policy"}
				}
				w.Header().Set("X-Otterio-IAM-Relationships", "v1")
				w.Header().Set("X-Otterio-IAM-Impact-Revision", after.Revision)
				w.Header().Set(iamBindingsCapability, "v1")
				w.Header().Set(iamBindingsRevision, afterBinding.Revision)
				json.NewEncoder(w).Encode(consoleapi.IAMRelationshipMutationResult{Outcome: "confirmed", Bindings: afterBinding, Impact: after})
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
			snapshot, err := client.IAMRelationships(context.Background(), "user", "native-parent")
			if err != nil {
				t.Fatal(err)
			}
			foreign := snapshot
			foreign.StorageTarget = "foreign-store"
			if _, err := client.IAMReplaceRelationshipBindings(context.Background(), binding, foreign, []string{"readonly"}); err == nil || writes.Load() != 0 {
				t.Fatal("foreign snapshot sent mutation")
			}
			result, err := client.IAMReplaceRelationshipBindings(context.Background(), binding, snapshot, []string{"readonly"})
			if writes.Load() != 1 {
				t.Fatal("mutation replayed", writes.Load())
			}
			if scenario == "confirmed" {
				if err != nil || result.Outcome != "confirmed" || result.Impact.StorageTarget != snapshot.StorageTarget {
					t.Fatalf("confirmed: %v", err)
				}
				return
			}
			var failure *consoleapi.Error
			if !errors.As(err, &failure) {
				t.Fatalf("expected structured failure: %v", err)
			}
			expected := "outcome_unknown"
			if scenario == "conflict" {
				expected = "identity_revision_conflict"
			}
			if failure.Code != expected {
				t.Fatalf("wrong retirement semantics: %s", failure.Code)
			}
		})
	}
}
