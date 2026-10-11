// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

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
	"github.com/soulteary/otterio/pkg/madmin"
)

func TestIAMRelationshipUserSecretEncryptionSingleDispatchAndUnknown(t *testing.T) {
	for _, scenario := range []string{"confirmed", "denied", "conflict", "unknown", "unexpected-impact", "missing-capability"} {
		t.Run(scenario, func(t *testing.T) {
			var writes atomic.Int32
			revision := strings.Repeat("a", 64)
			relation := consoleapi.IAMRelationships{Kind: "user", Target: "native-target", State: "known", CoupledUserSecret: scenario != "missing-capability", Revision: revision, ExpiresAt: time.Now().UTC().Add(30 * time.Second), AffectedAccessKeys: []string{"native-target", "native-child"}}
			binding := consoleapi.IAMBindings{Kind: "user", Target: "native-target", Conditional: true, Revision: revision, Policies: []string{}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.Header().Set("X-Otterio-IAM-Relationships", "v1")
					w.Header().Set("X-Otterio-IAM-Impact-Revision", revision)
					json.NewEncoder(w).Encode(relation)
					return
				}
				writes.Add(1)
				encrypted, _ := io.ReadAll(r.Body)
				if strings.Contains(string(encrypted), "new-target-secret") {
					t.Error("secret sent as cleartext")
				}
				plaintext, err := madmin.DecryptData(iamTestSecret, strings.NewReader(string(encrypted)))
				if err != nil {
					t.Error(err)
				}
				var fields map[string]string
				if json.Unmarshal(plaintext, &fields) != nil || len(fields) != 1 || fields["newSecretKey"] != "new-target-secret" {
					t.Error("secret-only request changed status or unrelated fields")
				}
				if r.URL.Path != "/otterio/admin/v3/iam-relationship-user-secret" {
					t.Error("wrong route")
				}
				for _, name := range []string{"x-otterio-iam-impact-if-match", "x-otterio-iam-impact-expires", strings.ToLower(iamBindingsCondition)} {
					if !strings.Contains(r.Header.Get("Authorization"), name) {
						t.Error("unsigned condition")
					}
				}
				switch scenario {
				case "denied":
					w.WriteHeader(403)
					w.Write([]byte(`{"Code":"AccessDenied"}`))
					return
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
				impact.ExpiresAt = time.Now().UTC().Add(30 * time.Second)
				if scenario == "unexpected-impact" {
					impact.AffectedAccessKeys = []string{"native-target"}
				}
				post := binding
				post.Revision = strings.Repeat("c", 64)
				w.Header().Set("X-Otterio-IAM-Relationships", "v1")
				w.Header().Set("X-Otterio-IAM-Impact-Revision", impact.Revision)
				w.Header().Set(iamBindingsCapability, "v1")
				w.Header().Set(iamBindingsRevision, post.Revision)
				json.NewEncoder(w).Encode(consoleapi.IAMRelationshipMutationResult{Outcome: "confirmed", Bindings: post, Impact: impact})
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
			snapshot, err := client.IAMRelationships(context.Background(), "user", "native-target")
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.IAMRotateRelationshipUserSecret(context.Background(), binding, snapshot, "bad")
			if err == nil || writes.Load() != 0 {
				t.Fatal("invalid secret dispatched")
			}
			result, err := client.IAMRotateRelationshipUserSecret(context.Background(), binding, snapshot, "new-target-secret")
			expectedWrites := int32(1)
			if scenario == "missing-capability" {
				expectedWrites = 0
			}
			if writes.Load() != expectedWrites {
				t.Fatal("wrong dispatch count", writes.Load())
			}
			if scenario == "confirmed" {
				if err != nil || result.Outcome != "confirmed" {
					t.Fatal(err)
				}
				return
			}
			var failure *consoleapi.Error
			if !errors.As(err, &failure) {
				t.Fatalf("expected structured failure: %v", err)
			}
			expected := "outcome_unknown"
			if scenario == "denied" {
				expected = "AccessDenied"
			}
			if scenario == "conflict" {
				expected = "identity_revision_conflict"
			}
			if scenario == "missing-capability" {
				expected = "identity_relationships_unknown"
			}
			if failure.Code != expected {
				t.Fatalf("wrong outcome classification %s", failure.Code)
			}
		})
	}
}
