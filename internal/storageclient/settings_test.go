// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/otterio/pkg/madmin"
)

var testSettingRevision = strings.Repeat("a", 64)
var testNextSettingRevision = strings.Repeat("b", 64)

func settingHeaders(w http.ResponseWriter, revision string) {
	w.Header().Set(configCapabilityHeader, "v1")
	w.Header().Set(configRevisionHeader, revision)
	w.Header().Set(configExistsHeader, "true")
}

func TestSettingsRawDocumentsAndSignedConditionalWrites(t *testing.T) {
	for _, test := range []struct{ kind, document string }{
		{"policy", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::bucket/public/*","Condition":{"StringLike":{"s3:prefix":["public/*"]}}}]}`},
		{"versioning", `<VersioningConfiguration><Status>Enabled</Status><ExcludedPrefixes><Prefix>logs/</Prefix></ExcludedPrefixes><FutureExtension attribute="preserve">unknown</FutureExtension></VersioningConfiguration>`},
		{"lifecycle", `<LifecycleConfiguration><Rule><ID>rule</ID><Status>Enabled</Status><Filter><Prefix>logs/</Prefix></Filter><Expiration><Days>30</Days></Expiration><FutureExtension>preserve</FutureExtension></Rule></LifecycleConfiguration>`},
	} {
		t.Run(test.kind, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.URL.Path != "/bucket/" || !r.URL.Query().Has(test.kind) {
					t.Errorf("unexpected setting target: %s", r.URL.Path)
				}
				if r.Header.Get("X-Amz-Security-Token") != "selected-session-token" || !strings.Contains(r.Header.Get("Authorization"), "Credential=selected-access/") {
					t.Error("setting request changed the selected identity")
				}
				if r.Method == http.MethodGet {
					revision := testSettingRevision
					if writes.Load() != 0 {
						revision = testNextSettingRevision
					}
					settingHeaders(w, revision)
					fmt.Fprint(w, test.document)
					return
				}
				writes.Add(1)
				if r.Method != http.MethodPut || r.Header.Get(configConditionHeader) != testSettingRevision || !strings.Contains(r.Header.Get("Authorization"), "x-otterio-config-if-match") {
					t.Error("conditional revision was missing or not signed")
				}
				data, err := io.ReadAll(r.Body)
				if err != nil || string(data) != test.document || r.ContentLength != int64(len(data)) {
					t.Error("full setting document was changed")
				}
				if test.kind != "policy" {
					checkPayloadHashes(t, r, data)
				}
				settingHeaders(w, testNextSettingRevision)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: "selected-secret-key", SessionToken: "selected-session-token"})
			setting, err := client.BucketSetting(context.Background(), "bucket", test.kind)
			if err != nil || setting.Document != test.document || !setting.Exists || !setting.Conditional || setting.Revision != testSettingRevision {
				t.Fatalf("raw read mismatch: %#v, %v", setting, err)
			}
			saved, err := client.SaveBucketSetting(context.Background(), "bucket", test.kind, test.document, setting.Revision, false)
			if err != nil || saved.Document != test.document || saved.Revision != testNextSettingRevision || writes.Load() != 1 {
				t.Fatalf("conditional save mismatch: %#v, %v, writes=%d", saved, err, writes.Load())
			}
		})
	}
}

func TestSettingsMissingCapabilityAndStaleRevisionPerformNoWrite(t *testing.T) {
	for _, test := range []struct {
		name, capability, serverRevision, clientRevision string
		wantStatus                                       int
		wantCode                                         string
	}{
		{"old-server", "", "", testSettingRevision, 501, "settings_unsupported"},
		{"unknown-capability", "v2", testSettingRevision, testSettingRevision, 501, "settings_unsupported"},
		{"invalid-revision", "v1", "not-a-revision", testSettingRevision, 501, "settings_unsupported"},
		{"stale", "v1", testNextSettingRevision, testSettingRevision, 409, "setting_conflict"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					// Discovery must not confer a setting capability.
					return
				}
				if r.Method != http.MethodGet {
					writes.Add(1)
				}
				w.Header().Set(configCapabilityHeader, test.capability)
				w.Header().Set(configRevisionHeader, test.serverRevision)
				w.Header().Set(configExistsHeader, "true")
				fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			_, err := client.SaveBucketSetting(context.Background(), "bucket", "policy", `{"Version":"2012-10-17","Statement":[]}`, test.clientRevision, false)
			assertAPIError(t, err, test.wantStatus, test.wantCode)
			if writes.Load() != 0 {
				t.Fatal("unsupported or stale configuration sent a write")
			}
		})
	}
}

func TestSettingsAbsentDocumentsCanBeCreatedAndDeletedConditionally(t *testing.T) {
	for _, kind := range []string{"policy", "lifecycle"} {
		t.Run(kind, func(t *testing.T) {
			var deletes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				settingHeaders(w, testSettingRevision)
				w.Header().Set(configExistsHeader, "false")
				if r.Method == http.MethodGet {
					code := "NoSuchBucketPolicy"
					if kind == "lifecycle" {
						code = "NoSuchLifecycleConfiguration"
					}
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprintf(w, `<Error><Code>%s</Code></Error>`, code)
					return
				}
				if r.Method != http.MethodDelete || r.Header.Get(configConditionHeader) != testSettingRevision || r.ContentLength != 0 {
					t.Error("conditional removal request changed")
				}
				deletes.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			setting, err := client.BucketSetting(context.Background(), "bucket", kind)
			if err != nil || setting.Exists || setting.Document != "" || !setting.Conditional {
				t.Fatalf("absent setting lost revision: %#v, %v", setting, err)
			}
			deleted, err := client.SaveBucketSetting(context.Background(), "bucket", kind, "", setting.Revision, true)
			if err != nil || deleted.Exists || deleted.Document != "" || deletes.Load() != 1 {
				t.Fatalf("remove mismatch: %#v, %v", deleted, err)
			}
		})
	}
}

func TestSettingsServerConflictAndLostAcknowledgmentAreNotRetried(t *testing.T) {
	for _, test := range []struct {
		name, response string
		status         int
		wantStatus     int
		wantCode       string
	}{
		{"server-cas-conflict", `<Error><Code>PreconditionFailed</Code><Message>secret-marker</Message></Error>`, 412, 409, "setting_conflict"},
		{"denied", `<Error><Code>AccessDenied</Code><Message>secret-marker</Message></Error>`, 403, 403, "AccessDenied"},
		{"malformed-error", `secret-marker`, 500, 502, "outcome_unknown"},
		{"parsed-internal-error", `<Error><Code>InternalError</Code><Message>secret-marker</Message></Error>`, 500, 502, "outcome_unknown"},
		{"parsed-unavailable", `<Error><Code>ServiceUnavailable</Code></Error>`, 503, 502, "outcome_unknown"},
		{"not-implemented", `<Error><Code>NotImplemented</Code></Error>`, 501, 501, "NotSupported"},
		{"missing-ack-revision", ``, 204, 502, "outcome_unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Method == http.MethodGet {
					settingHeaders(w, testSettingRevision)
					fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
					return
				}
				writes.Add(1)
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.response)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			_, err := client.SaveBucketSetting(context.Background(), "bucket", "policy", `{"Version":"2012-10-17","Statement":[]}`, testSettingRevision, false)
			assertAPIError(t, err, test.wantStatus, test.wantCode)
			if writes.Load() != 1 || strings.Contains(err.Error(), "secret-marker") {
				t.Fatal("failed setting was retried or exposed upstream data")
			}
		})
	}
}

func TestSettingsNativeLifecycleTargetRejectionIsKnownOnlyWithMatchingStatus(t *testing.T) {
	const current = `<LifecycleConfiguration><Rule><ID>existing</ID><Status>Disabled</Status><Filter><Prefix>logs/</Prefix></Filter><Expiration><Days>30</Days></Expiration></Rule></LifecycleConfiguration>`
	const submitted = `<LifecycleConfiguration><Rule><Status>Disabled</Status><Filter><Prefix>logs/</Prefix></Filter><NoncurrentVersionTransition><NoncurrentDays>30</NoncurrentDays><StorageClass>unconfigured-archive</StorageClass></NoncurrentVersionTransition></Rule></LifecycleConfiguration>`
	for _, tc := range []struct {
		name, body, wantCode string
		status, wantStatus   int
		incomplete           bool
	}{
		{"missing-target", `<Error><Code>XOtterioAdminRemoteTargetNotFoundError</Code><Message>secret-marker</Message></Error>`, "XOtterioAdminRemoteTargetNotFoundError", 404, 404, false},
		{"missing-destination", `<Error><Code>RemoteDestinationNotFoundError</Code><Message>secret-marker</Message></Error>`, "RemoteDestinationNotFoundError", 404, 404, false},
		{"wrong-status", `<Error><Code>XOtterioAdminRemoteTargetNotFoundError</Code></Error>`, "outcome_unknown", 403, 502, false},
		{"persistence-error", `<Error><Code>XOtterioAdminRemoteTargetNotFoundError</Code></Error>`, "outcome_unknown", 500, 502, false},
		{"duplicate-error-code", `<Error><Code>XOtterioAdminRemoteTargetNotFoundError</Code><Code>InternalError</Code></Error>`, "outcome_unknown", 404, 502, false},
		{"trailing-error-root", `<Error><Code>XOtterioAdminRemoteTargetNotFoundError</Code></Error><Error/>`, "outcome_unknown", 404, 502, false},
		{"error-directive", `<!DOCTYPE Error><Error><Code>XOtterioAdminRemoteTargetNotFoundError</Code></Error>`, "outcome_unknown", 404, 502, false},
		{"incomplete-error-body", `<Error><Code>XOtterioAdminRemoteTargetNotFoundError</Code></Error>`, "outcome_unknown", 404, 502, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Method == http.MethodGet {
					settingHeaders(w, testSettingRevision)
					fmt.Fprint(w, current)
					return
				}
				writes.Add(1)
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != submitted || r.Header.Get(configConditionHeader) != testSettingRevision {
					t.Error("lifecycle rejection fixture did not receive the exact protected document")
				}
				if tc.incomplete {
					w.Header().Set("Content-Length", fmt.Sprint(len(tc.body)+1))
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			_, err := client.SaveBucketSetting(context.Background(), "bucket", "lifecycle", submitted, testSettingRevision, false)
			assertAPIError(t, err, tc.wantStatus, tc.wantCode)
			if writes.Load() != 1 || strings.Contains(err.Error(), "secret-marker") {
				t.Fatal("a rejected/uncertain lifecycle write was retried or exposed upstream data")
			}
			reloaded, err := client.BucketSetting(context.Background(), "bucket", "lifecycle")
			if err != nil || reloaded.Document != current || reloaded.Revision != testSettingRevision {
				t.Fatalf("rejection fixture changed the previous document/revision: %+v error=%v", reloaded, err)
			}
		})
	}
}

func TestSettingsPreserveDNSBucketAddressAndDiscoveredRegion(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("location") {
			fmt.Fprint(w, `<LocationConstraint>us-west-2</LocationConstraint>`)
			return
		}
		if !strings.HasPrefix(r.Host, "bucket.storage.example:") || r.URL.Path != "/" || !strings.Contains(r.Header.Get("Authorization"), "/us-west-2/s3/aws4_request") {
			t.Errorf("lost DNS lookup or discovered region: %s %s", r.Host, r.URL.Path)
		}
		settingHeaders(w, testSettingRevision)
		if r.Method == http.MethodPut {
			writes.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
	}))
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")
	_, port, _ := net.SplitHostPort(address)
	client := testClient(t, Config{S3URL: "http://storage.example:" + port, Path: "off"})
	client.s3Transport.Proxy = nil
	client.s3Transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}
	_, err := client.SaveBucketSetting(context.Background(), "bucket", "policy", `{"Version":"2012-10-17","Statement":[]}`, testSettingRevision, false)
	if err != nil || writes.Load() != 1 {
		t.Fatalf("DNS conditional write failed: %v", err)
	}
}

func TestSettingsV2CanReadButCannotSendUnsignedCondition(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		settingHeaders(w, testSettingRevision)
		fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL, API: "s3v2"})
	setting, err := client.BucketSetting(context.Background(), "bucket", "policy")
	if err != nil || setting.Document == "" || setting.Conditional || setting.Revision != "" {
		t.Fatalf("V2 read failed: %v", err)
	}
	_, err = client.SaveBucketSetting(context.Background(), "bucket", "policy", setting.Document, testSettingRevision, false)
	assertAPIError(t, err, 501, "settings_unsupported")
	if writes.Load() != 0 {
		t.Fatal("V2 sent an unsigned configuration condition")
	}
}

func TestSettingsParallelReadsDoNotShareCapabilities(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.URL.Path == "/unsupported/" {
			close(firstStarted)
			<-releaseFirst
		} else {
			settingHeaders(w, testSettingRevision)
		}
		fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		setting, err := client.BucketSetting(context.Background(), "unsupported", "policy")
		if err != nil || setting.Conditional || setting.Revision != "" {
			t.Error("parallel response granted another call a capability")
		}
	}()
	<-firstStarted
	setting, err := client.BucketSetting(context.Background(), "supported", "policy")
	if err != nil || !setting.Conditional {
		t.Errorf("supported read lost capability: %v", err)
	}
	close(releaseFirst)
	wg.Wait()
}

func TestSelfAccountRotationUsesEncryptedSelectedIdentityAndNoRetry(t *testing.T) {
	var puts atomic.Int32
	const oldSecret = "selected-old-secret"
	const newSecret = "selected-new-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/otterio/admin/v3/self-credentials" || !strings.Contains(r.Header.Get("Authorization"), "Credential=selected-access/") || r.Header.Get("X-Amz-Security-Token") != "selected-token" {
			t.Error("self request changed admin address or startup identity")
		}
		w.Header().Set(selfCapabilityHeader, "v1")
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"kind":"iam","status":"enabled","canRotateSecret":true,"secretKey":"must-not-escape"}`)
			return
		}
		puts.Add(1)
		data, err := madmin.DecryptData(oldSecret, r.Body)
		var payload map[string]string
		if err != nil || json.Unmarshal(data, &payload) != nil || payload["newSecretKey"] != newSecret || len(payload) != 1 {
			t.Error("rotation payload was not encrypted with the selected old secret")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL, AdminURL: server.URL, AccessKey: "selected-access", SecretKey: oldSecret, SessionToken: "selected-token"})
	account, err := client.SelfAccount(context.Background())
	if err != nil || !account.CanRotateSecret {
		t.Fatalf("self account read failed: %#v, %v", account, err)
	}
	data, _ := json.Marshal(account)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "must-not-escape") {
		t.Fatal("self DTO exposed credentials")
	}
	if err := client.RotateOwnSecret(context.Background(), newSecret); err != nil || puts.Load() != 1 {
		t.Fatalf("rotation failed or retried: %v", err)
	}
}

func TestSelfAccountUnsupportedPrincipalNeverRotates(t *testing.T) {
	for _, test := range []struct{ kind, status, capability string }{
		{"iam", "enabled", ""}, {"root", "enabled", "v1"}, {"sts", "enabled", "v1"}, {"service", "enabled", "v1"}, {"iam", "disabled", "v1"}, {"future-kind", "enabled", "v1"},
	} {
		t.Run(test.kind+"-"+test.status+"-"+test.capability, func(t *testing.T) {
			var puts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					puts.Add(1)
				}
				w.Header().Set(selfCapabilityHeader, test.capability)
				fmt.Fprintf(w, `{"kind":%q,"status":%q,"canRotateSecret":true}`, test.kind, test.status)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			assertAPIError(t, client.RotateOwnSecret(context.Background(), "selected-new-secret"), 501, "secret_rotation_unsupported")
			if puts.Load() != 0 {
				t.Fatal("unsupported principal changed credentials")
			}
		})
	}
}

func TestSelfRotationLostResponseAndRedirectAreNotRetried(t *testing.T) {
	for _, mode := range []string{"lost", "redirect", "missing-capability"} {
		t.Run(mode, func(t *testing.T) {
			var puts atomic.Int32
			var redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.Header().Set(selfCapabilityHeader, "v1")
					fmt.Fprint(w, `{"kind":"iam","status":"enabled","canRotateSecret":true}`)
					return
				}
				puts.Add(1)
				switch mode {
				case "lost":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						conn.Close()
					}
				case "redirect":
					// A proxy can replace an upstream success after the write
					// has committed. Not following it cannot establish failure.
					if _, err := madmin.DecryptData("secret-marker-test-key", r.Body); err != nil {
						t.Error("rotation did not dispatch its encrypted body")
					}
					w.Header().Set("Location", target.URL)
					w.WriteHeader(http.StatusTemporaryRedirect)
				case "missing-capability":
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			assertAPIError(t, client.RotateOwnSecret(context.Background(), "selected-new-secret"), 502, "outcome_unknown")
			if puts.Load() != 1 || redirected.Load() != 0 {
				t.Fatal("rotation retried or followed a credential-bearing redirect")
			}
		})
	}
}

func TestSettingsAndSelfMetadataTimeoutBoundCompleteReply(t *testing.T) {
	for _, operation := range []string{"setting", "self"} {
		t.Run(operation, func(t *testing.T) {
			ended := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if operation == "self" {
					w.Header().Set(selfCapabilityHeader, "v1")
				}
				fmt.Fprint(w, "{")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(ended)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			client.metadataTimeout = 50 * time.Millisecond
			var err error
			if operation == "setting" {
				_, err = client.BucketSetting(context.Background(), "bucket", "policy")
			} else {
				_, err = client.SelfAccount(context.Background())
			}
			assertAPIError(t, err, 504, "Timeout")
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("stalled metadata body was not canceled")
			}
		})
	}
}

func TestSettingsAndSelfAuthenticationFailureDoesNotDispatchWrite(t *testing.T) {
	for _, code := range []string{"AccessDenied", "SignatureDoesNotMatch", "InvalidToken"} {
		t.Run(code, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Method != http.MethodGet {
					writes.Add(1)
				}
				w.WriteHeader(http.StatusForbidden)
				if r.URL.Path == "/otterio/admin/v3/self-credentials" {
					// Absence of capability is the old-server degradation case.
					fmt.Fprintf(w, `{"Code":%q,"Message":"credential-marker"}`, code)
				} else {
					fmt.Fprintf(w, `<Error><Code>%s</Code><Message>credential-marker</Message></Error>`, code)
				}
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			_, err := client.SaveBucketSetting(context.Background(), "bucket", "policy", `{"Version":"2012-10-17","Statement":[]}`, testSettingRevision, false)
			assertAPIError(t, err, 403, "AccessDenied")
			assertAPIError(t, client.RotateOwnSecret(context.Background(), "selected-new-secret"), 403, "AccessDenied")
			if writes.Load() != 0 || strings.Contains(err.Error(), "credential-marker") {
				t.Fatal("failed authentication dispatched a write or leaked upstream error data")
			}
		})
	}
}

func TestSettingsValidateBeforeNetworkAndBoundReplySizes(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if locationResponse(w, r) {
			return
		}
		if r.URL.Path == "/otterio/admin/v3/self-credentials" {
			w.Header().Set(selfCapabilityHeader, "v1")
			fmt.Fprint(w, strings.Repeat("x", maxSelfReply+1))
			return
		}
		fmt.Fprint(w, strings.Repeat("x", maxSettingDocument+1))
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	for _, test := range []struct {
		bucket, kind, document, revision string
		remove                           bool
	}{
		{"bad bucket", "policy", "{}", testSettingRevision, false},
		{"bucket", "unknown", "{}", testSettingRevision, false},
		{"bucket", "policy", "{}", "bad-revision", false},
		{"bucket", "policy", "[]", testSettingRevision, false},
		{"bucket", "policy", "null", testSettingRevision, false},
		{"bucket", "policy", "{\"Version\":\"2012-10-17\",\"Statement\":[],\"Id\":\"\xff\"}", testSettingRevision, false},
		{"bucket", "policy", strings.Repeat(" ", 20*1024+1), testSettingRevision, false},
		{"bucket", "versioning", `<WrongRoot/>`, testSettingRevision, false},
		{"bucket", "versioning", `<VersioningConfiguration/><VersioningConfiguration/>`, testSettingRevision, false},
		{"bucket", "lifecycle", `<!DOCTYPE LifecycleConfiguration><LifecycleConfiguration/>`, testSettingRevision, false},
		{"bucket", "versioning", "", testSettingRevision, true},
		{"bucket", "policy", "{}", testSettingRevision, true},
	} {
		_, err := client.SaveBucketSetting(context.Background(), test.bucket, test.kind, test.document, test.revision, test.remove)
		assertAPIError(t, err, 400, "InvalidRequest")
	}
	for _, secret := range []string{"short", strings.Repeat("a", 129), "1234567\n", "1234567\x00", "1234567\xff"} {
		assertAPIError(t, client.RotateOwnSecret(context.Background(), secret), 400, "InvalidRequest")
	}
	if requests.Load() != 0 {
		t.Fatal("invalid setting or secret input reached the network")
	}
	_, err := client.BucketSetting(context.Background(), "bucket", "policy")
	assertAPIError(t, err, 502, "UpstreamError")
	_, err = client.SelfAccount(context.Background())
	assertAPIError(t, err, 502, "UpstreamError")
}

func TestSettingsEmptyVersioningRetainsStandardXMLAndAbsentFlag(t *testing.T) {
	const document = `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"/>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		settingHeaders(w, testSettingRevision)
		w.Header().Set(configExistsHeader, "false")
		fmt.Fprint(w, document)
	}))
	defer server.Close()
	setting, err := testClient(t, Config{S3URL: server.URL}).BucketSetting(context.Background(), "bucket", "versioning")
	if err != nil || setting.Exists || !setting.Conditional || setting.Document != document {
		t.Fatalf("empty versioning mismatch: %#v, %v", setting, err)
	}
}

func TestSelfRotationCancellationAfterDispatchHasUnknownOutcome(t *testing.T) {
	started, ended := make(chan struct{}), make(chan struct{})
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(selfCapabilityHeader, "v1")
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"kind":"iam","status":"enabled","canRotateSecret":true}`)
			return
		}
		puts.Add(1)
		// Consume the encrypted payload so cancellation reaches the server's
		// in-flight response rather than its unread request body.
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "{")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(ended)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- client.RotateOwnSecret(ctx, "selected-new-secret") }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("rotation did not dispatch")
	}
	cancel()
	select {
	case err := <-result:
		assertAPIError(t, err, 502, "outcome_unknown")
	case <-time.After(time.Second):
		t.Fatal("rotation ignored cancellation")
	}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("canceled rotation left an active upstream response")
	}
	if puts.Load() != 1 {
		t.Fatal("canceled rotation was retried")
	}
}

func TestSelfMalformedDiscoveryCannotRetireCredentials(t *testing.T) {
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
		w.Header().Set(selfCapabilityHeader, "v1")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "credential-marker")
	}))
	defer server.Close()
	err := testClient(t, Config{S3URL: server.URL}).RotateOwnSecret(context.Background(), "selected-new-secret")
	assertAPIError(t, err, 502, "UpstreamError")
	if puts.Load() != 0 || strings.Contains(err.Error(), "credential-marker") {
		t.Fatal("malformed discovery dispatched a rotation or leaked its body")
	}
}

func TestSelfRotationParsedPersistenceErrorsRetireOldIdentity(t *testing.T) {
	for _, test := range []struct {
		code   string
		status int
	}{
		{"InternalError", 500},
		{"CredentialPropagationIncomplete", 503},
		{"ServiceUnavailable", 503},
		{"CredentialConflict", 409},
	} {
		t.Run(test.code, func(t *testing.T) {
			var puts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(selfCapabilityHeader, "v1")
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `{"kind":"iam","status":"enabled","canRotateSecret":true}`)
					return
				}
				puts.Add(1)
				w.WriteHeader(test.status)
				fmt.Fprintf(w, `{"Code":%q,"Message":"private-persistence-marker"}`, test.code)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			err := client.RotateOwnSecret(context.Background(), "selected-new-secret")
			assertAPIError(t, err, 502, "outcome_unknown")
			if puts.Load() != 1 || strings.Contains(err.Error(), "private-persistence-marker") {
				t.Fatal("persistence error retried rotation or exposed a private message")
			}
		})
	}
}

func TestSelfLegacyUnsupportedDoesNotReadUnknownLargeOrStalledBody(t *testing.T) {
	for _, status := range []int{200, 400, 404, 501} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			ended := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, strings.Repeat("legacy-page-marker", 1024))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(ended)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			client.metadataTimeout = 500 * time.Millisecond
			started := time.Now()
			account, err := client.SelfAccount(context.Background())
			if err != nil || account.Kind != "unknown" || account.CanRotateSecret || time.Since(started) > 250*time.Millisecond {
				t.Fatalf("legacy discovery read its untrusted body: %#v, %v", account, err)
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("legacy discovery left the response open")
			}
		})
	}
}

func TestSettingsConfirmedWriteReadsActualCanonicalDocument(t *testing.T) {
	const submitted = `{ "Statement": [], "Version": "2012-10-17" }`
	const canonical = `{"Version":"2012-10-17","Statement":[]}`
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			if string(body) != submitted {
				t.Error("submitted document was transformed before writing")
			}
			writes.Add(1)
			settingHeaders(w, testNextSettingRevision)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if writes.Load() == 0 {
			settingHeaders(w, testSettingRevision)
			fmt.Fprint(w, submitted)
		} else {
			settingHeaders(w, testNextSettingRevision)
			fmt.Fprint(w, canonical)
		}
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	setting, err := client.SaveBucketSetting(context.Background(), "bucket", "policy", submitted, testSettingRevision, false)
	if err != nil || setting.Document != canonical || setting.Revision != testNextSettingRevision || writes.Load() != 1 {
		t.Fatalf("displayed unsaved input or replayed write: %#v, %v", setting, err)
	}
}

func TestSettingsNoncurrentLifecyclePreservesTargetsAndConflicts(t *testing.T) {
	const submitted = `<?xml version="1.0" encoding="UTF-8"?><LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Rule><ID>archive-versions</ID><Filter><And><Prefix>logs/</Prefix><Tag><Key>环境</Key><Value>生产+归档</Value></Tag></And></Filter><Status>Enabled</Status><Transition><Days>30</Days><StorageClass>current-archive</StorageClass></Transition><NoncurrentVersionTransition><NoncurrentDays>7</NoncurrentDays><StorageClass>historical-archive</StorageClass></NoncurrentVersionTransition><NoncurrentVersionExpiration><NoncurrentDays>365</NoncurrentDays></NoncurrentVersionExpiration></Rule>
</LifecycleConfiguration>`
	const canonical = `<LifecycleConfiguration><Rule><ID>archive-versions</ID><Status>Enabled</Status><Filter><And><Prefix>logs/</Prefix><Tag><Key>环境</Key><Value>生产+归档</Value></Tag></And></Filter><Transition><Days>30</Days><StorageClass>current-archive</StorageClass></Transition><NoncurrentVersionExpiration><NoncurrentDays>365</NoncurrentDays></NoncurrentVersionExpiration><NoncurrentVersionTransition><NoncurrentDays>7</NoncurrentDays><StorageClass>historical-archive</StorageClass></NoncurrentVersionTransition></Rule></LifecycleConfiguration>`
	for _, mode := range []string{"confirmed", "stale-preflight", "write-conflict", "post-commit-conflict"} {
		t.Run(mode, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.URL.Path != "/bucket/" || !r.URL.Query().Has("lifecycle") {
					t.Errorf("configuration changed target: %s", r.URL)
				}
				if r.Method == http.MethodPut {
					body, err := io.ReadAll(r.Body)
					if err != nil || string(body) != submitted || r.Header.Get(configConditionHeader) != testSettingRevision {
						t.Errorf("full XML, distinct targets or CAS revision changed: %s", body)
					}
					checkPayloadHashes(t, r, body)
					if !strings.Contains(r.Header.Get("Authorization"), "x-otterio-config-if-match") {
						t.Error("noncurrent configuration omitted signed CAS header")
					}
					writes.Add(1)
					if mode == "write-conflict" {
						w.WriteHeader(http.StatusPreconditionFailed)
						fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code></Error>`)
						return
					}
					settingHeaders(w, testNextSettingRevision)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if writes.Load() == 0 {
					revision := testSettingRevision
					if mode == "stale-preflight" {
						revision = testNextSettingRevision
					}
					settingHeaders(w, revision)
					fmt.Fprint(w, submitted)
				} else {
					revision := testNextSettingRevision
					if mode == "post-commit-conflict" {
						revision = strings.Repeat("c", 64)
					}
					settingHeaders(w, revision)
					fmt.Fprint(w, canonical)
				}
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: "selected-secret-key"})
			if mode == "confirmed" {
				read, err := client.BucketSetting(context.Background(), "bucket", "lifecycle")
				if err != nil || read.Document != submitted || read.Revision != testSettingRevision {
					t.Fatalf("typed SDK dropped raw noncurrent document fields: %#v %v", read, err)
				}
			}
			saved, err := client.SaveBucketSetting(context.Background(), "bucket", "lifecycle", submitted, testSettingRevision, false)
			if mode == "confirmed" {
				if err != nil || saved.Document != canonical || saved.Revision != testNextSettingRevision || !saved.Exists || !saved.Conditional {
					t.Fatalf("canonical noncurrent configuration was not confirmed: %#v %v", saved, err)
				}
			} else {
				assertAPIError(t, err, 409, "setting_conflict")
			}
			wantWrites := int32(1)
			if mode == "stale-preflight" {
				wantWrites = 0
			}
			if writes.Load() != wantWrites {
				t.Fatalf("configuration conflict replayed or dispatched a stale write: got %d want %d", writes.Load(), wantWrites)
			}
		})
	}
}

func TestSettingsSupportedLegacyLifecycleRootUsesFullRawDocument(t *testing.T) {
	const document = `<BucketLifecycleConfiguration><Rule><ID>old-root</ID><Status>Enabled</Status><Filter><Prefix>logs/</Prefix></Filter><NoncurrentVersionTransition><NoncurrentDays>7</NoncurrentDays><StorageClass>historical-archive</StorageClass></NoncurrentVersionTransition></Rule></BucketLifecycleConfiguration>`
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			if string(body) != document {
				t.Error("legacy lifecycle document was rewritten before storage validation")
			}
			writes.Add(1)
			settingHeaders(w, testNextSettingRevision)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		revision := testSettingRevision
		if writes.Load() > 0 {
			revision = testNextSettingRevision
		}
		settingHeaders(w, revision)
		fmt.Fprint(w, document)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: "selected-secret-key"})
	setting, err := client.BucketSetting(context.Background(), "bucket", "lifecycle")
	if err != nil || setting.Document != document || setting.Revision != testSettingRevision || !setting.Conditional {
		t.Fatalf("SDK decoding blocked the server-supported raw lifecycle root: %#v %v", setting, err)
	}
	saved, err := client.SaveBucketSetting(context.Background(), "bucket", "lifecycle", document, setting.Revision, false)
	if err != nil || saved.Document != document || saved.Revision != testNextSettingRevision || writes.Load() != 1 {
		t.Fatalf("server-supported lifecycle root could not be conditionally resaved: %#v %v writes=%d", saved, err, writes.Load())
	}
}

func TestSettingsLifecycleRawReadRejectsIncompleteOrErrorReplies(t *testing.T) {
	const document = `<BucketLifecycleConfiguration><Rule><ID>old-root</ID><Status>Enabled</Status><Filter><Prefix>logs/</Prefix></Filter><NoncurrentVersionTransition><NoncurrentDays>7</NoncurrentDays><StorageClass>historical-archive</StorageClass></NoncurrentVersionTransition></Rule></BucketLifecycleConfiguration>`
	canonical := strings.ReplaceAll(document, "BucketLifecycleConfiguration", "LifecycleConfiguration")
	for _, test := range []struct {
		name, body  string
		status      int
		wrongLength bool
	}{
		{"http-error-with-configuration", document, http.StatusForbidden, false},
		{"error-document-with-success", `<Error><Code>AccessDenied</Code></Error>`, http.StatusOK, false},
		{"multiple-roots", document + document, http.StatusOK, false},
		{"directive", `<!DOCTYPE BucketLifecycleConfiguration>` + document, http.StatusOK, false},
		{"trailing-data", document + `trailing`, http.StatusOK, false},
		{"incomplete-xml", strings.TrimSuffix(document, `</BucketLifecycleConfiguration>`), http.StatusOK, false},
		{"truncated-transport", document, http.StatusOK, true},
		{"canonical-multiple-roots", canonical + canonical, http.StatusOK, false},
		{"canonical-directive", `<!DOCTYPE LifecycleConfiguration>` + canonical, http.StatusOK, false},
		{"canonical-trailing-data", canonical + `trailing`, http.StatusOK, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Method != http.MethodGet {
					writes.Add(1)
				}
				settingHeaders(w, testSettingRevision)
				if test.wrongLength {
					w.Header().Set("Content-Length", fmt.Sprint(len(test.body)+5))
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: "selected-secret-key"})
			setting, err := client.BucketSetting(context.Background(), "bucket", "lifecycle")
			if err == nil || setting.Conditional || setting.Document != "" || setting.Revision != "" {
				t.Fatalf("raw lifecycle read trusted an incomplete/error response: %#v %v", setting, err)
			}
			_, err = client.SaveBucketSetting(context.Background(), "bucket", "lifecycle", document, testSettingRevision, false)
			if err == nil || writes.Load() != 0 {
				t.Fatalf("incomplete/error read authorized a configuration write: %v writes=%d", err, writes.Load())
			}
		})
	}
}

func TestSettingsLifecycleMalformedPostCommitReadNeverReplaysWrite(t *testing.T) {
	const document = `<LifecycleConfiguration><Rule><Status>Enabled</Status><Filter><Prefix>logs/</Prefix></Filter><NoncurrentVersionTransition><NoncurrentDays>7</NoncurrentDays><StorageClass>historical-archive</StorageClass></NoncurrentVersionTransition></Rule></LifecycleConfiguration>`
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.Method == http.MethodPut {
			writes.Add(1)
			settingHeaders(w, testNextSettingRevision)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if writes.Load() == 0 {
			settingHeaders(w, testSettingRevision)
			fmt.Fprint(w, document)
			return
		}
		settingHeaders(w, testNextSettingRevision)
		fmt.Fprint(w, document+document)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL, AccessKey: "selected-access", SecretKey: "selected-secret-key"})
	_, err := client.SaveBucketSetting(context.Background(), "bucket", "lifecycle", document, testSettingRevision, false)
	assertAPIError(t, err, http.StatusBadGateway, "outcome_unknown")
	if writes.Load() != 1 {
		t.Fatal("malformed post-commit document replayed the configuration write")
	}
}

func TestSettingsPostCommitReadFailureNeverReplaysWrite(t *testing.T) {
	for _, mode := range []string{"denied", "lost", "changed", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Method == http.MethodPut {
					writes.Add(1)
					settingHeaders(w, testNextSettingRevision)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if writes.Load() == 0 {
					settingHeaders(w, testSettingRevision)
				} else {
					switch mode {
					case "denied":
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
						return
					case "lost":
						conn, _, err := w.(http.Hijacker).Hijack()
						if err == nil {
							conn.Close()
						}
						return
					case "changed":
						settingHeaders(w, strings.Repeat("c", 64))
					case "unsupported":
					}
				}
				fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			_, err := client.SaveBucketSetting(context.Background(), "bucket", "policy", `{"Version":"2012-10-17","Statement":[]}`, testSettingRevision, false)
			if mode == "changed" {
				assertAPIError(t, err, 409, "setting_conflict")
			} else {
				assertAPIError(t, err, 502, "outcome_unknown")
			}
			if writes.Load() != 1 {
				t.Fatal("post-commit read failure replayed the configuration write")
			}
		})
	}
}

func TestSettingsAmbiguousCapabilityNeverDispatchesWrite(t *testing.T) {
	for _, header := range []string{configCapabilityHeader, configRevisionHeader, configExistsHeader} {
		t.Run(header, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Method != http.MethodGet {
					writes.Add(1)
				}
				settingHeaders(w, testSettingRevision)
				w.Header().Add(header, "contradictory-value")
				fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			_, err := client.SaveBucketSetting(context.Background(), "bucket", "policy", `{"Version":"2012-10-17","Statement":[]}`, testSettingRevision, false)
			assertAPIError(t, err, 501, "settings_unsupported")
			if writes.Load() != 0 {
				t.Fatal("ambiguous capability metadata dispatched a write")
			}
		})
	}
}

func TestSettingsContradictoryExistenceNeverDispatchesWrite(t *testing.T) {
	for _, kind := range []string{"policy", "lifecycle"} {
		for _, absent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-absent-%v", kind, absent), func(t *testing.T) {
				var writes atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if locationResponse(w, r) {
						return
					}
					if r.Method != http.MethodGet {
						writes.Add(1)
					}
					settingHeaders(w, testSettingRevision)
					if absent {
						w.WriteHeader(http.StatusNotFound)
						code := "NoSuchBucketPolicy"
						if kind == "lifecycle" {
							code = "NoSuchLifecycleConfiguration"
						}
						fmt.Fprintf(w, `<Error><Code>%s</Code></Error>`, code)
					} else {
						w.Header().Set(configExistsHeader, "false")
						if kind == "policy" {
							fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
						} else {
							fmt.Fprint(w, `<LifecycleConfiguration><Rule><ID>rule</ID><Status>Enabled</Status><Filter><Prefix>logs/</Prefix></Filter><Expiration><Days>30</Days></Expiration></Rule></LifecycleConfiguration>`)
						}
					}
				}))
				defer server.Close()
				client := testClient(t, Config{S3URL: server.URL})
				_, err := client.SaveBucketSetting(context.Background(), "bucket", kind, "", testSettingRevision, true)
				assertAPIError(t, err, 502, "UpstreamError")
				if writes.Load() != 0 {
					t.Fatal("contradictory existence metadata dispatched a write")
				}
			})
		}
	}
}

func TestSelfDiscoveryRedirectDoesNotDispatchOrRetire(t *testing.T) {
	var writes, redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	assertAPIError(t, client.RotateOwnSecret(context.Background(), "selected-new-secret"), 502, "RedirectDisabled")
	if writes.Load() != 0 || redirected.Load() != 0 {
		t.Fatal("read-only redirect dispatched a rotation or followed the new host")
	}
}

func TestSettingsAmbiguousAcknowledgementNeverConfirmsWrite(t *testing.T) {
	for _, header := range []string{configCapabilityHeader, configRevisionHeader, configExistsHeader, "wrong-existence", "accepted", "unexpected-body"} {
		t.Run(header, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Method == http.MethodGet {
					settingHeaders(w, testSettingRevision)
					fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
					return
				}
				_, _ = io.Copy(io.Discard, r.Body)
				writes.Add(1)
				settingHeaders(w, testSettingRevision)
				switch header {
				case "wrong-existence":
					w.Header().Set(configExistsHeader, "false")
				case "accepted":
					w.WriteHeader(http.StatusAccepted)
					return
				case "unexpected-body":
					fmt.Fprint(w, `<html>proxy replaced the upstream reply</html>`)
					return
				default:
					w.Header().Add(header, "contradictory-value")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			_, err := client.SaveBucketSetting(context.Background(), "bucket", "policy", `{"Version":"2012-10-17","Statement":[]}`, testSettingRevision, false)
			assertAPIError(t, err, 502, "outcome_unknown")
			if writes.Load() != 1 {
				t.Fatal("ambiguous acknowledgement replayed a write")
			}
		})
	}
}

func TestSettingsUntrustedErrorEnvelopeCannotProveMutationFailure(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"proxy-html", `<html><Code>AccessDenied</Code></html>`, 403},
		{"proxy-precondition", `<html>proxy refused the response</html>`, 412},
		{"duplicate-code", `<Error><Code>InternalError</Code><Code>AccessDenied</Code></Error>`, 403},
		{"trailing-document", `<Error><Code>AccessDenied</Code></Error><proxy/>`, 403},
		{"unknown-code", `<Error><Code>ProxyResponseBlocked</Code></Error>`, 403},
		{"post-commit-redirect", ``, 307},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Method == http.MethodGet {
					settingHeaders(w, testSettingRevision)
					fmt.Fprint(w, `{"Version":"2012-10-17","Statement":[]}`)
					return
				}
				_, _ = io.Copy(io.Discard, r.Body)
				writes.Add(1)
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			_, err := client.SaveBucketSetting(context.Background(), "bucket", "policy", `{"Version":"2012-10-17","Statement":[]}`, testSettingRevision, false)
			assertAPIError(t, err, 502, "outcome_unknown")
			if writes.Load() != 1 {
				t.Fatal("untrusted error replayed a configuration write")
			}
		})
	}
}

func TestSelfUntrustedErrorEnvelopeCannotProveMutationFailure(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"proxy-html", `<html><Code>AccessDenied</Code></html>`, 403},
		{"duplicate-code", `{"Code":"CredentialConflict","Code":"AccessDenied"}`, 409},
		{"unknown-code", `{"Code":"ProxyResponseBlocked"}`, 403},
		{"wrong-status", `{"Code":"NotImplemented"}`, 404},
		{"ambiguous-capability", ``, 204},
		{"unexpected-success-body", `<html>proxy replaced the upstream reply</html>`, 200},
		{"accepted", ``, 202},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(selfCapabilityHeader, "v1")
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `{"kind":"iam","status":"enabled","canRotateSecret":true}`)
					return
				}
				if _, err := madmin.DecryptData("secret-marker-test-key", r.Body); err != nil {
					t.Error("rotation did not dispatch its encrypted body")
				}
				writes.Add(1)
				if test.name == "ambiguous-capability" {
					w.Header().Add(selfCapabilityHeader, "v2")
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			assertAPIError(t, client.RotateOwnSecret(context.Background(), "selected-new-secret"), 502, "outcome_unknown")
			if writes.Load() != 1 {
				t.Fatal("untrusted error replayed a secret rotation")
			}
		})
	}
}

func TestSelfAmbiguousDiscoveryNeverDispatchesWrite(t *testing.T) {
	for _, test := range []struct {
		name, body, code string
		status           int
	}{
		{"duplicate-kind", `{"kind":"sts","kind":"iam","status":"enabled","canRotateSecret":true}`, "UpstreamError", 502},
		{"duplicate-status", `{"kind":"iam","status":"disabled","status":"enabled","canRotateSecret":true}`, "UpstreamError", 502},
		{"duplicate-permission", `{"kind":"iam","status":"enabled","canRotateSecret":false,"canRotateSecret":true}`, "UpstreamError", 502},
		{"case-aliases", `{"Kind":"iam","Status":"enabled","CanRotateSecret":true}`, "secret_rotation_unsupported", 501},
		{"ambiguous-capability", `{"kind":"iam","status":"enabled","canRotateSecret":true}`, "secret_rotation_unsupported", 501},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes.Add(1)
				}
				w.Header().Set(selfCapabilityHeader, "v1")
				if test.name == "ambiguous-capability" {
					w.Header().Add(selfCapabilityHeader, "v2")
				}
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			assertAPIError(t, client.RotateOwnSecret(context.Background(), "selected-new-secret"), test.status, test.code)
			if writes.Load() != 0 {
				t.Fatal("ambiguous account discovery granted a rotation")
			}
		})
	}
}

func TestSelfDiscoveryAuthenticationFailureClosesUnreadBody(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		for _, capability := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d-capability-%v", status, capability), func(t *testing.T) {
				var writes atomic.Int32
				ended := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						writes.Add(1)
					}
					if capability {
						w.Header().Set(selfCapabilityHeader, "v1")
					}
					w.WriteHeader(status)
					fmt.Fprint(w, `<html>`+strings.Repeat("denial-page", maxSelfReply))
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					close(ended)
				}))
				defer server.Close()
				client := testClient(t, Config{S3URL: server.URL})
				client.metadataTimeout = 50 * time.Millisecond
				assertAPIError(t, client.RotateOwnSecret(context.Background(), "selected-new-secret"), 403, "AccessDenied")
				if writes.Load() != 0 {
					t.Fatal("denied discovery dispatched a rotation")
				}
				select {
				case <-ended:
				case <-time.After(time.Second):
					t.Fatal("denied discovery retained its unread response")
				}
			})
		}
	}
}
