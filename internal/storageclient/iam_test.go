// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/madmin"
)

const iamTestSecret = "selected-secret-key"

func iamEncrypted(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	data, err = madmin.EncryptData(iamTestSecret, data)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(data)
}

func iamDiscovery(w http.ResponseWriter, r *http.Request) bool {
	if strings.HasSuffix(r.URL.Path, "/self-credentials") {
		w.Header().Set(selfCapabilityHeader, "v1")
		fmt.Fprint(w, `{"kind":"iam","status":"enabled","canRotateSecret":true}`)
		return true
	}
	return false
}

func TestIAMListsRedactCredentialsAndPreservePolicies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "Credential=selected-access/") || r.Header.Get("X-Amz-Security-Token") != "selected-session-token" {
			t.Error("IAM changed selected identity")
		}
		switch r.URL.Path {
		case "/otterio/admin/v3/list-users":
			iamEncrypted(t, w, map[string]madmin.UserInfo{"alice": {SecretKey: "server-secret-never-browser", Status: madmin.AccountEnabled, PolicyName: "readwrite,readonly", MemberOf: []string{"team"}}})
		case "/otterio/admin/v3/groups":
			fmt.Fprint(w, `["team"]`)
		case "/otterio/admin/v3/group":
			fmt.Fprint(w, `{"name":"team","status":"enabled","members":["alice"],"policy":"readonly"}`)
		case "/otterio/admin/v3/list-service-accounts":
			iamEncrypted(t, w, madmin.ListServiceAccountsResp{Accounts: []string{"service-key"}})
		case "/otterio/admin/v3/info-service-account":
			iamEncrypted(t, w, madmin.InfoServiceAccountResp{ParentUser: "alice", AccountStatus: "on", ImpliedPolicy: true})
		case "/otterio/admin/v3/list-canned-policies":
			fmt.Fprint(w, `{"readonly":{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::bucket/*"]}]}}`)
		default:
			t.Error("unexpected IAM path", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret, SessionToken: "selected-session-token"})
	users, err := c.IAMUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(users)
	if strings.Contains(string(data), "server-secret") || len(users.Users) != 1 || users.Users[0].Policies[0] != "readonly" {
		t.Fatalf("unsafe users %s", data)
	}
	groups, err := c.IAMGroups(context.Background())
	if err != nil || len(groups.Groups) != 1 || len(groups.Groups[0].Members) != 1 {
		t.Fatalf("groups %v %v", groups, err)
	}
	accounts, err := c.IAMServiceAccounts(context.Background(), "alice")
	if err != nil || len(accounts.ServiceAccounts) != 1 || accounts.ServiceAccounts[0].ParentUser != "alice" {
		t.Fatalf("service accounts %v %v", accounts, err)
	}
	policies, err := c.IAMPolicies(context.Background())
	if err != nil || len(policies.Policies) != 1 || !strings.Contains(policies.Policies[0].Document, "s3:GetObject") {
		t.Fatalf("policies %v %v", policies, err)
	}
}

func TestIAMSecretRotationSendsOnlyEncryptedSecret(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if iamDiscovery(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/list-users") {
			iamEncrypted(t, w, map[string]madmin.UserInfo{"alice": {Status: madmin.AccountDisabled}})
			return
		}
		if r.URL.Path != "/otterio/admin/v3/iam-user-secret" || r.URL.Query().Get("target") != "alice" {
			t.Error("rotation used an unsafe upsert", r.URL)
			w.WriteHeader(404)
			return
		}
		w.Header().Set("X-Otterio-IAM-User-Secret", "v1")
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"kind":"user","target":"alice","secretOnly":true}`)
			return
		}
		writes.Add(1)
		data, err := madmin.DecryptData(iamTestSecret, r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var fields map[string]string
		if json.Unmarshal(data, &fields) != nil || len(fields) != 1 || fields["newSecretKey"] != "replacement-secret" {
			t.Error("rotation sent status/type alongside secret")
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
	result, err := c.IAMAction(context.Background(), consoleapi.IAMActionRequest{Action: "user.rotate", User: "alice", SecretKey: "replacement-secret", ConfirmTarget: "alice"})
	if err != nil || writes.Load() != 1 || result.RestartRequired || result.Credentials != nil {
		t.Fatalf("mutation %v %v", result, err)
	}
}

func TestIAMRejectsRootNonemptyTargetsAndUnsafeBindingsBeforeWrite(t *testing.T) {
	for _, test := range []struct {
		name, action, target string
		status               int
		code                 string
	}{
		{"root", "user.disable", "selected-access", 403, "immutable_root"},
		{"user-with-keys", "user.delete", "alice", 409, "user_has_service_accounts"},
		{"user-with-groups", "user.delete", "alice", 409, "user_has_groups"},
		{"nonempty-group", "group.delete", "team", 409, "group_not_empty"},
		{"legacy-bindings", "user.policies", "alice", 501, "policy_bindings_unsupported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes.Add(1)
					w.WriteHeader(500)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/self-credentials") {
					w.Header().Set(selfCapabilityHeader, "v1")
					kind := "iam"
					if test.name == "root" {
						kind = "root"
					}
					fmt.Fprintf(w, `{"kind":%q,"status":"enabled"}`, kind)
					return
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/list-users"):
					info := madmin.UserInfo{Status: madmin.AccountEnabled}
					iamEncrypted(t, w, map[string]madmin.UserInfo{"alice": info})
				case strings.HasSuffix(r.URL.Path, "/list-service-accounts"):
					iamEncrypted(t, w, madmin.ListServiceAccountsResp{Accounts: []string{"service"}})
				case strings.HasSuffix(r.URL.Path, "/groups"):
					fmt.Fprint(w, `["team"]`)
				case strings.HasSuffix(r.URL.Path, "/group"):
					fmt.Fprint(w, `{"name":"team","members":["alice"]}`)
				case strings.HasSuffix(r.URL.Path, "/user-info"):
					if test.name == "user-with-groups" {
						fmt.Fprint(w, `{"status":"enabled","memberOf":["team"]}`)
					} else {
						fmt.Fprint(w, `{"status":"enabled","policyName":"readonly"}`)
					}
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
			args := consoleapi.IAMActionRequest{Action: test.action, User: test.target, ConfirmTarget: test.target}
			if test.action == "user.policies" {
				args.Policies = []string{"readonly"}
				args.Revision = strings.Repeat("a", 64)
			}
			if strings.HasPrefix(test.action, "group.") {
				args.User = ""
				args.Group = test.target
			}
			_, err := c.IAMAction(context.Background(), args)
			assertAPIError(t, err, test.status, test.code)
			if writes.Load() != 0 {
				t.Fatal("unsafe target reached mutation")
			}
		})
	}
}

func TestIAMLostAndMalformedAcknowledgementsNeverRetryOrLeakSecrets(t *testing.T) {
	for _, status := range []int{500, 200, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if iamDiscovery(w, r) {
					return
				}
				if strings.HasSuffix(r.URL.Path, "/list-users") {
					iamEncrypted(t, w, map[string]madmin.UserInfo{"alice": {Status: madmin.AccountEnabled}})
					return
				}
				if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/iam-user-secret") {
					w.Header().Set("X-Otterio-IAM-User-Secret", "v1")
					fmt.Fprint(w, `{"kind":"user","target":"alice","secretOnly":true}`)
					return
				}
				writes.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(status)
				if status == 403 {
					fmt.Fprint(w, `{"Code":"AccessDenied","Message":"supplied-secret-unsafe"}`)
				} else {
					fmt.Fprint(w, "supplied-secret-unsafe")
				}
			}))
			defer server.Close()
			c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
			c.metadataTimeout = 2 * time.Second
			_, err := c.IAMAction(context.Background(), consoleapi.IAMActionRequest{Action: "user.rotate", User: "alice", SecretKey: "supplied-secret-unsafe", ConfirmTarget: "alice"})
			if status == 403 {
				assertAPIError(t, err, 403, "AccessDenied")
			} else {
				assertAPIError(t, err, 502, "outcome_unknown")
			}
			if writes.Load() != 1 || strings.Contains(err.Error(), "supplied-secret") {
				t.Fatalf("replayed or leaked mutation: writes=%d %v", writes.Load(), err)
			}
		})
	}
}

func TestIAMCreationRequiresNativeCapabilityAndNeverUpserts(t *testing.T) {
	for _, kind := range []string{"user", "group"} {
		for _, mode := range []string{"supported", "legacy", "race"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				var writes atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if iamDiscovery(w, r) {
						return
					}
					if strings.HasSuffix(r.URL.Path, "/list-users") {
						iamEncrypted(t, w, map[string]madmin.UserInfo{})
						return
					}
					if strings.HasSuffix(r.URL.Path, "/groups") {
						fmt.Fprint(w, `[]`)
						return
					}
					if r.URL.Path != "/otterio/admin/v3/iam-create" {
						t.Error("creation used an unsafe SDK upsert", r.URL.Path)
						w.WriteHeader(500)
						return
					}
					if r.URL.Query().Get("kind") != kind || r.URL.Query().Get("target") != "new-target" {
						t.Error("create target changed")
					}
					if r.Method == http.MethodGet {
						if mode == "legacy" {
							w.WriteHeader(404)
							return
						}
						w.Header().Set("X-Otterio-IAM-Create", "v1")
						fmt.Fprintf(w, `{"kind":%q,"target":"new-target","createOnly":true}`, kind)
						return
					}
					writes.Add(1)
					data, err := madmin.DecryptData(iamTestSecret, r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					var body struct {
						SecretKey string   `json:"secretKey"`
						Members   []string `json:"members"`
					}
					if json.Unmarshal(data, &body) != nil || (kind == "user" && len(body.SecretKey) != 40) || (kind == "group" && (body.SecretKey != "" || len(body.Members) != 1 || body.Members[0] != "alice")) {
						t.Error("create encrypted payload changed")
					}
					if mode == "race" {
						w.WriteHeader(412)
						fmt.Fprint(w, `{"Code":"PreconditionFailed","Message":"secret-never-echo"}`)
						return
					}
					w.Header().Set("X-Otterio-IAM-Create", "v1")
					w.WriteHeader(204)
				}))
				defer server.Close()
				c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
				args := consoleapi.IAMActionRequest{Action: kind + ".create", ConfirmTarget: "new-target"}
				if kind == "user" {
					args.User = "new-target"
				} else {
					args.Group = "new-target"
					args.Members = []string{"alice"}
				}
				result, err := c.IAMAction(context.Background(), args)
				switch mode {
				case "legacy":
					assertAPIError(t, err, 501, "iam_create_unsupported")
					if writes.Load() != 0 {
						t.Fatal("legacy server received upsert")
					}
				case "race":
					assertAPIError(t, err, 409, "iam_exists")
					if result.Credentials != nil || writes.Load() != 1 {
						t.Fatal("failed create disclosed credential or replayed")
					}
				default:
					if err != nil || writes.Load() != 1 {
						t.Fatalf("creation: %v %v", result, err)
					}
					if kind == "user" && (result.Credentials == nil || result.Credentials.AccessKey != "new-target" || len(result.Credentials.SecretKey) != 40) {
						t.Fatal("generated secret not returned once")
					}
				}
			})
		}
	}
}

func TestIAMBindingWritesSignLoadedRevisionAndRejectStaleDrafts(t *testing.T) {
	for _, mode := range []string{"success", "stale", "server-conflict"} {
		t.Run(mode, func(t *testing.T) {
			var writes atomic.Int32
			revision := strings.Repeat("a", 64)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if iamDiscovery(w, r) {
					return
				}
				if r.URL.Path != "/otterio/admin/v3/iam-policy-bindings" {
					t.Error("unexpected binding path", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				w.Header().Set(iamBindingsCapability, "v1")
				if r.Method == http.MethodGet {
					current := revision
					if mode == "stale" {
						current = strings.Repeat("b", 64)
					}
					w.Header().Set(iamBindingsRevision, current)
					fmt.Fprintf(w, `{"kind":"user","target":"alice","policies":["readonly"],"revision":%q,"conditional":true}`, current)
					return
				}
				writes.Add(1)
				if r.Method != http.MethodPut || r.Header.Get(iamBindingsCondition) != revision || !strings.Contains(r.Header.Get("Authorization"), "x-otterio-iam-if-match") {
					t.Error("revision missing or unsigned")
				}
				data, _ := io.ReadAll(r.Body)
				if string(data) != `{"policies":["readwrite"]}` {
					t.Error("direct binding replacement changed", string(data))
				}
				if mode == "server-conflict" {
					w.WriteHeader(412)
					fmt.Fprint(w, `{"Code":"PreconditionFailed"}`)
					return
				}
				next := strings.Repeat("c", 64)
				w.Header().Set(iamBindingsRevision, next)
				fmt.Fprintf(w, `{"kind":"user","target":"alice","policies":["readwrite"],"revision":%q,"conditional":true}`, next)
			}))
			defer server.Close()
			c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
			_, err := c.IAMAction(context.Background(), consoleapi.IAMActionRequest{Action: "user.policies", User: "alice", ConfirmTarget: "alice", Policies: []string{"readwrite"}, Revision: revision})
			if mode == "success" {
				if err != nil || writes.Load() != 1 {
					t.Fatalf("CAS %v writes%d", err, writes.Load())
				}
			} else {
				assertAPIError(t, err, 409, "binding_conflict")
				if (mode == "stale" && writes.Load() != 0) || (mode == "server-conflict" && writes.Load() != 1) {
					t.Fatal("CAS sent/replayed incorrect number of writes")
				}
			}
		})
	}
}

func TestIAMServiceAccountSDKPayloadsAndSecretDisclosure(t *testing.T) {
	policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::bucket/*"]}]}`
	for _, action := range []string{"create-generated", "create-both-generated", "create-supplied", "enable", "disable", "rotate", "policy"} {
		t.Run(action, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if iamDiscovery(w, r) {
					return
				}
				if strings.HasSuffix(r.URL.Path, "/info-service-account") {
					iamEncrypted(t, w, madmin.InfoServiceAccountResp{ParentUser: "alice", AccountStatus: "on", ImpliedPolicy: true})
					return
				}
				writes.Add(1)
				data, err := madmin.DecryptData(iamTestSecret, r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if strings.HasPrefix(action, "create-") {
					if r.Method != http.MethodPut || !strings.HasSuffix(r.URL.Path, "/add-service-account") {
						t.Error("wrong service create SDK endpoint")
					}
					var request madmin.AddServiceAccountReq
					expectedAccessKey := "new-service"
					if action == "create-both-generated" {
						expectedAccessKey = ""
					}
					if json.Unmarshal(data, &request) != nil || request.TargetUser != "alice" || request.Policy == nil || request.AccessKey != expectedAccessKey {
						t.Error("service parent/restriction changed")
					}
					secret := "generated-service-secret"
					if action == "create-supplied" {
						secret = "supplied-service-secret"
						if request.SecretKey != secret {
							t.Error("supplied secret changed")
						}
					} else if request.SecretKey != "" {
						t.Error("generation unexpectedly supplied secret")
					}
					iamEncrypted(t, w, madmin.AddServiceAccountResp{Credentials: auth.Credentials{AccessKey: "new-service", SecretKey: secret, ParentUser: "alice"}})
					return
				}
				if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/update-service-account") || r.URL.Query().Get("accessKey") != "service-key" {
					t.Error("wrong service update SDK endpoint")
				}
				var request madmin.UpdateServiceAccountReq
				if json.Unmarshal(data, &request) != nil {
					t.Error("invalid encrypted update")
				}
				switch action {
				case "enable":
					if request.NewStatus != "on" || request.NewSecretKey != "" || request.NewPolicy != nil {
						t.Error("enable changed unrelated fields")
					}
				case "disable":
					if request.NewStatus != "off" {
						t.Error("disable status wrong")
					}
				case "rotate":
					if request.NewSecretKey != "replacement-service-secret" || request.NewPolicy != nil || request.NewStatus != "" {
						t.Error("rotation changed policy/status")
					}
				case "policy":
					if request.NewPolicy == nil || request.NewSecretKey != "" || request.NewStatus != "" {
						t.Error("policy changed secret/status")
					}
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
			args := consoleapi.IAMActionRequest{Action: "service-account." + action, AccessKey: "service-key", ConfirmTarget: "service-key"}
			if strings.HasPrefix(action, "create-") {
				args.Action = "service-account.create"
				args.User = "alice"
				args.AccessKey = "new-service"
				if action == "create-both-generated" {
					args.AccessKey = ""
				}
				args.ConfirmTarget = "alice"
				args.Policy = policy
				if action == "create-supplied" {
					args.SecretKey = "supplied-service-secret"
				}
			}
			if action == "rotate" {
				args.SecretKey = "replacement-service-secret"
			}
			if action == "policy" {
				args.Policy = policy
			}
			result, err := c.IAMAction(context.Background(), args)
			if err != nil || writes.Load() != 1 {
				t.Fatalf("service action %v %v", result, err)
			}
			if action == "create-generated" || action == "create-both-generated" {
				if result.Credentials == nil || result.Credentials.SecretKey != "generated-service-secret" {
					t.Fatal("generated service secret missing")
				}
			} else if result.Credentials != nil {
				t.Fatal("existing/supplied service secret exposed")
			}
		})
	}
}

func TestIAMSecretRotationRequiresServerSecretOnlyCapability(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
			w.WriteHeader(500)
			return
		}
		if iamDiscovery(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/list-users") {
			iamEncrypted(t, w, map[string]madmin.UserInfo{"alice": {Status: madmin.AccountEnabled}})
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
	_, err := c.IAMAction(context.Background(), consoleapi.IAMActionRequest{Action: "user.rotate", User: "alice", SecretKey: "replacement-secret", ConfirmTarget: "alice"})
	assertAPIError(t, err, 501, "iam_secret_unsupported")
	if writes.Load() != 0 {
		t.Fatal("legacy server received unsafe user upsert for rotation")
	}
}

func TestIAMServiceAccountSuppliedSecretRequiresExplicitAccessKey(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
	_, err := c.IAMAction(context.Background(), consoleapi.IAMActionRequest{Action: "service-account.create", User: "alice", SecretKey: "supplied-service-secret", ConfirmTarget: "alice"})
	assertAPIError(t, err, 400, "InvalidRequest")
	if requests.Load() != 0 {
		t.Fatal("partial credential input reached storage server")
	}
}

func TestIAMBindingsRejectTemporaryExternalAndUnknownIdentitiesBeforeWrite(t *testing.T) {
	for _, kind := range []string{"sts", "directory", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			var writes atomic.Int32
			revision := strings.Repeat("a", 64)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes.Add(1)
					w.WriteHeader(500)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/self-credentials") {
					w.Header().Set(selfCapabilityHeader, "v1")
					fmt.Fprintf(w, `{"kind":%q,"status":"enabled"}`, kind)
					return
				}
				w.Header().Set(iamBindingsCapability, "v1")
				w.Header().Set(iamBindingsRevision, revision)
				fmt.Fprintf(w, `{"kind":"user","target":"alice","policies":[],"revision":%q,"conditional":true}`, revision)
			}))
			defer server.Close()
			c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
			_, err := c.IAMAction(context.Background(), consoleapi.IAMActionRequest{Action: "user.policies", User: "alice", ConfirmTarget: "alice", Policies: []string{"readonly"}, Revision: revision})
			assertAPIError(t, err, 501, "iam_unsupported")
			if writes.Load() != 0 {
				t.Fatal("unsupported identity reached conditional binding mutation")
			}
		})
	}
}

func TestIAMUserListsKeepOmittedMembershipUnknownWithoutAdditionalPermissions(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/otterio/admin/v3/list-users" {
			t.Error("list required additional per-user permission", r.URL.Path)
			w.WriteHeader(403)
			return
		}
		iamEncrypted(t, w, map[string]any{
			"unknown-user": map[string]any{"status": "enabled"},
			"empty-user":   map[string]any{"status": "enabled", "memberOf": []string{}},
			"grouped-user": map[string]any{"status": "enabled", "memberOf": []string{"team"}},
		})
	}))
	defer server.Close()
	c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
	result, err := c.IAMUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || len(result.Users) != 3 {
		t.Fatal("unexpected list fanout")
	}
	for _, user := range result.Users {
		switch user.AccessKey {
		case "unknown-user":
			if user.MemberOfKnown || user.MemberOf != nil {
				t.Fatal("omitted membership became confirmed empty")
			}
		case "empty-user":
			if !user.MemberOfKnown || user.MemberOf == nil || len(user.MemberOf) != 0 {
				t.Fatal("explicit empty membership lost")
			}
		case "grouped-user":
			if !user.MemberOfKnown || len(user.MemberOf) != 1 || user.MemberOf[0] != "team" {
				t.Fatal("known group membership lost")
			}
		}
	}
}

func TestIAMUserDeleteRequiresAuthoritativeMembershipPermissionBeforeDispatch(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
			w.WriteHeader(500)
			return
		}
		if iamDiscovery(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/list-users") {
			iamEncrypted(t, w, map[string]madmin.UserInfo{"alice": {Status: madmin.AccountEnabled}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/user-info") {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"Code":"AccessDenied"}`)
			return
		}
		t.Error("deletion continued after missing authoritative user info", r.URL.Path)
		w.WriteHeader(500)
	}))
	defer server.Close()
	c := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: iamTestSecret})
	_, err := c.IAMAction(context.Background(), consoleapi.IAMActionRequest{Action: "user.delete", User: "alice", ConfirmTarget: "alice"})
	assertAPIError(t, err, 403, "AccessDenied")
	if writes.Load() != 0 {
		t.Fatal("user deleted without authoritative membership permission")
	}
}
