// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"bytes"
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

func TestServiceAccountMutationEncryptedSingleDispatchAndExactParentScope(t *testing.T) {
	for _, scenario := range []string{"confirmed", "rotate", "conflict", "unknown", "changed-parent", "expanded-impact", "missing-capability", "wrong-state", "policy-confirmed", "policy-wrong"} {
		t.Run(scenario, func(t *testing.T) {
			var writes atomic.Int32
			relation := consoleapi.IAMRelationships{Kind: "service-account", Target: "service-key", NativeParent: "native-parent", State: "known", CoupledServiceAccounts: true, Revision: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(20 * time.Second), AffectedAccessKeys: []string{"service-key"}}
			action := "service-account.disable"
			policy := ""
			secret := ""
			if scenario == "rotate" {
				action = "service-account.rotate"
				secret = "new-service-secret"
			}
			if strings.HasPrefix(scenario, "policy-") {
				action = "service-account.policy"
				policy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::fixture/*"]}]}`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writes.Add(1)
				if r.Method != "PUT" || r.URL.Path != "/otterio/admin/v3/iam-relationship-service-accounts" || r.URL.Query().Get("kind") != "service-account" || r.URL.Query().Get("target") != "service-key" {
					t.Error("wrong service account protocol")
				}
				for _, header := range []string{"x-otterio-iam-impact-if-match", "x-otterio-iam-impact-expires"} {
					if !strings.Contains(r.Header.Get("Authorization"), header) {
						t.Error("unsigned graph condition")
					}
				}
				encrypted, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(encrypted, []byte("service-key")) || (secret != "" && bytes.Contains(encrypted, []byte(secret))) {
					t.Error("unencrypted service mutation")
				}
				body, err := madmin.DecryptData(iamTestSecret, bytes.NewReader(encrypted))
				if err != nil {
					t.Error(err)
					return
				}
				var args map[string]any
				if json.Unmarshal(body, &args) != nil || args["action"] != action || args["target"] != "service-key" {
					t.Error("wrong encrypted body")
				}
				if secret != "" && args["newSecretKey"] != secret {
					t.Error("rotation secret not encrypted")
				}
				if r.Header.Get("X-Otterio-IAM-Impact-If-Match") != relation.Revision {
					t.Error("wrong graph revision")
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
				account := consoleapi.IAMServiceAccount{AccessKey: "service-key", ParentUser: "native-parent", Status: "off", ImpliedPolicy: true}
				if policy != "" {
					account.ImpliedPolicy = false
					account.Policy = policy
					if scenario == "policy-wrong" {
						account.Policy = strings.ReplaceAll(policy, "s3:GetObject", "s3:*")
					}
				}
				switch scenario {
				case "changed-parent":
					impact.NativeParent = "different-parent"
					account.ParentUser = "different-parent"
				case "expanded-impact":
					impact.AffectedAccessKeys = []string{"service-key", "native-parent"}
				case "missing-capability":
					impact.CoupledServiceAccounts = false
				case "wrong-state":
					account.Status = "on"
				}
				w.Header().Set("X-Otterio-IAM-Relationships", "v1")
				w.Header().Set("X-Otterio-IAM-Impact-Revision", impact.Revision)
				json.NewEncoder(w).Encode(consoleapi.IAMServiceAccountMutationResult{Outcome: "confirmed", ServiceAccount: account, Impact: impact})
			}))
			defer server.Close()
			client, err := New(Config{S3URL: server.URL, AdminURL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret, API: "S3v4", Path: "on"})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			relation.StorageTarget = client.IAMStorageTarget()
			result, err := client.IAMMutateRelationshipServiceAccount(context.Background(), consoleapi.IAMActionRequest{Action: action, AccessKey: "service-key", ConfirmTarget: "service-key", SecretKey: secret, Policy: policy}, relation)
			if writes.Load() != 1 {
				t.Fatal("service mutation retried", writes.Load())
			}
			if scenario == "confirmed" || scenario == "rotate" || scenario == "policy-confirmed" {
				if err != nil || result.Outcome != "confirmed" {
					t.Fatal(err)
				}
				encoded, _ := json.Marshal(result)
				if secret != "" && bytes.Contains(encoded, []byte(secret)) {
					t.Fatal("secret reflected to browser DTO")
				}
				return
			}
			var failure *consoleapi.Error
			if !errors.As(err, &failure) {
				t.Fatal("missing typed result", err)
			}
			want := "outcome_unknown"
			if scenario == "conflict" {
				want = "identity_revision_conflict"
			}
			if failure.Code != want {
				t.Fatal(failure.Code, want)
			}
		})
	}
}
func TestServiceAccountMutationRefusesUnknownParentAndUnrelatedImpactWithoutDispatch(t *testing.T) {
	var dispatches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { dispatches.Add(1) }))
	defer server.Close()
	client, err := New(Config{S3URL: server.URL, AdminURL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret, API: "S3v4", Path: "on"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	base := consoleapi.IAMRelationships{Kind: "service-account", Target: "service-key", NativeParent: "native-parent", State: "known", CoupledServiceAccounts: true, Revision: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(20 * time.Second), AffectedAccessKeys: []string{"service-key"}, StorageTarget: client.IAMStorageTarget()}
	for _, scenario := range []string{"create", "no-parent", "self-parent", "expanded-scope", "expired", "no-capability", "invalid-policy", "wrong-target"} {
		relation := base
		args := consoleapi.IAMActionRequest{Action: "service-account.disable", AccessKey: "service-key", ConfirmTarget: "service-key"}
		switch scenario {
		case "create":
			args.Action = "service-account.create"
		case "no-parent":
			relation.NativeParent = ""
		case "self-parent":
			relation.NativeParent = "service-key"
		case "expanded-scope":
			relation.AffectedAccessKeys = []string{"service-key", "sibling"}
		case "expired":
			relation.ExpiresAt = time.Now().Add(-time.Second)
		case "no-capability":
			relation.CoupledServiceAccounts = false
		case "invalid-policy":
			args.Action = "service-account.policy"
			args.Policy = "invalid"
		case "wrong-target":
			relation.StorageTarget = "other"
		}
		if _, err := client.IAMMutateRelationshipServiceAccount(context.Background(), args, relation); err == nil {
			t.Fatal(scenario, "accepted")
		}
	}
	if dispatches.Load() != 0 {
		t.Fatal("untrusted service mutation dispatched")
	}
}
