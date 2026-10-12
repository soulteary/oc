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
	"github.com/soulteary/otterio/pkg/madmin"
)

func TestRelationshipServiceCreationSingleDispatchProtectedAck(t *testing.T) {
	for _, scenario := range []string{"confirmed", "supplied-secret", "missing-marker", "duplicate-marker", "wrong-parent", "wrong-key", "session-token", "expiration", "disabled", "duplicate-credential", "unexpected-field", "conflict", "stale-proof", "collision", "unknown-conflict", "unknown", "denied"} {
		t.Run(scenario, func(t *testing.T) {
			var writes atomic.Int32
			relation := consoleapi.IAMRelationships{Kind: "user", Target: "parent", State: "known", CoupledServiceCreation: true, Revision: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(20 * time.Second), AffectedAccessKeys: []string{"parent"}}
			args := consoleapi.IAMActionRequest{Action: "service-account.create", User: "parent", AccessKey: "new-child", ConfirmTarget: "parent"}
			if scenario == "supplied-secret" {
				args.SecretKey = "supplied-secret-123"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writes.Add(1)
				if r.Method != "PUT" || r.URL.Path != "/otterio/admin/v3/add-service-account" {
					t.Error("wrong protected creation route")
				}
				for _, name := range []string{"x-otterio-iam-service-creation", "x-otterio-iam-impact-if-match", "x-otterio-iam-impact-expires"} {
					if !strings.Contains(r.Header.Get("Authorization"), name) {
						t.Error("unsigned creation condition", name)
					}
				}
				if r.Header.Get(nativeServiceCreationHeader) != "v1" || r.Header.Get("X-Otterio-IAM-Impact-If-Match") != relation.Revision {
					t.Error("missing creation condition")
				}
				decrypted, err := madmin.DecryptData(iamTestSecret, r.Body)
				if err != nil {
					t.Error("request not encrypted")
					return
				}
				var req madmin.AddServiceAccountReq
				if json.Unmarshal(decrypted, &req) != nil || req.TargetUser != "parent" || req.AccessKey != "new-child" || req.SecretKey != args.SecretKey {
					t.Error("unexpected creation payload")
				}
				if scenario == "conflict" || scenario == "stale-proof" || scenario == "collision" {
					w.WriteHeader(412)
					w.Write([]byte(`{"Code":"PreconditionFailed"}`))
					return
				}
				if scenario == "unknown-conflict" {
					w.WriteHeader(503)
					w.Write([]byte(`{"Code":"PreconditionFailed"}`))
					return
				}
				if scenario == "unknown" {
					w.WriteHeader(503)
					w.Write([]byte(`{"Code":"IAMPropagationIncomplete"}`))
					return
				}
				if scenario == "denied" {
					w.WriteHeader(403)
					w.Write([]byte(`{"Code":"XOtterioIAMActionNotAllowed"}`))
					return
				}
				if scenario != "missing-marker" {
					w.Header().Set(nativeServiceCreationHeader, "v1")
				}
				if scenario == "duplicate-marker" {
					w.Header().Add(nativeServiceCreationHeader, "v1")
				}
				credential := map[string]any{"accessKey": "new-child", "secretKey": "generated-secret-123", "parentUser": "parent"}
				if scenario == "supplied-secret" {
					credential["secretKey"] = args.SecretKey
				}
				switch scenario {
				case "wrong-parent":
					credential["parentUser"] = "other"
				case "wrong-key":
					credential["accessKey"] = "other-child"
				case "session-token":
					credential["sessionToken"] = "temporary"
				case "expiration":
					credential["expiration"] = time.Now().Add(time.Minute)
				case "disabled":
					credential["status"] = "off"
				}
				if scenario == "unexpected-field" {
					credential["extra"] = true
				}
				if scenario == "duplicate-credential" {
					encrypted, _ := madmin.EncryptData(iamTestSecret, []byte(`{"credentials":{"accessKey":"new-child","secretKey":"generated-secret-123","parentUser":"other","parentUser":"parent"}}`))
					w.Write(encrypted)
					return
				}
				iamEncrypted(t, w, map[string]any{"credentials": credential})
			}))
			defer server.Close()
			client, err := New(Config{S3URL: server.URL, AdminURL: server.URL, AccessKey: "actor", SecretKey: iamTestSecret, API: "S3v4", Path: "on"})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			relation.StorageTarget = client.IAMStorageTarget()
			result, err := client.IAMCreateRelationshipServiceAccount(context.Background(), args, relation)
			if writes.Load() != 1 {
				t.Fatal("creation repeated or missing", writes.Load())
			}
			if scenario == "confirmed" || scenario == "supplied-secret" {
				if err != nil || result.Outcome != "confirmed" || (scenario == "confirmed" && (result.Credentials == nil || result.Credentials.AccessKey != "new-child")) || (scenario == "supplied-secret" && result.Credentials != nil) {
					t.Fatal("invalid creation acknowledgement", err)
				}
				return
			}
			var failure *consoleapi.Error
			code := "outcome_unknown"
			if scenario == "conflict" || scenario == "stale-proof" || scenario == "collision" {
				code = "identity_revision_conflict"
			}
			if scenario == "denied" {
				code = "iam_action_denied"
			}
			if !errors.As(err, &failure) || failure.Code != code || result.Credentials != nil {
				t.Fatal("unsafe creation result", err)
			}
		})
	}
}

func TestRelationshipServiceCreationMissingCapabilityNeverDispatches(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{S3URL: server.URL, AdminURL: server.URL, AccessKey: "actor", SecretKey: iamTestSecret, API: "S3v4", Path: "on"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, scenario := range []string{"missing-cap", "expired", "wrong-parent", "wrong-namespace", "missing-parent-scope"} {
		relation := consoleapi.IAMRelationships{Kind: "user", Target: "parent", State: "known", CoupledServiceCreation: true, StorageTarget: client.IAMStorageTarget(), Revision: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(time.Minute), AffectedAccessKeys: []string{"parent"}}
		switch scenario {
		case "missing-cap":
			relation.CoupledServiceCreation = false
		case "expired":
			relation.ExpiresAt = time.Now().Add(-time.Second)
		case "wrong-parent":
			relation.Target = "other"
		case "wrong-namespace":
			relation.StorageTarget = "other"
		case "missing-parent-scope":
			relation.AffectedAccessKeys = []string{"child"}
		}
		_, err := client.IAMCreateRelationshipServiceAccount(context.Background(), consoleapi.IAMActionRequest{Action: "service-account.create", User: "parent", AccessKey: "new-child", ConfirmTarget: "parent"}, relation)
		if err == nil {
			t.Fatal("untrusted create accepted", scenario)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unsafe legacy creation dispatched", calls.Load())
	}
}
