// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeAuthenticationRequiresPositiveEnabledIAMProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, header, body string
		status             int
		allowed            bool
	}{
		{"iam", "v1", `{"kind":"iam","status":"enabled"}`, 200, true},
		{"root", "v1", `{"kind":"root","status":"enabled"}`, 200, false},
		{"sts", "v1", `{"kind":"sts","status":"enabled"}`, 200, false},
		{"service", "v1", `{"kind":"service","status":"enabled"}`, 200, false},
		{"directory", "v1", `{"kind":"directory","status":"enabled"}`, 200, false},
		{"disabled", "v1", `{"kind":"iam","status":"disabled"}`, 200, false},
		{"missing-status", "v1", `{"kind":"iam"}`, 200, false},
		{"wrong-case", "v1", `{"Kind":"iam","Status":"enabled"}`, 200, false},
		{"duplicate-field", "v1", `{"kind":"root","kind":"iam","status":"enabled"}`, 200, false},
		{"missing-protocol", "", `{"kind":"iam","status":"enabled"}`, 200, false},
		{"future-protocol", "v2", `{"kind":"iam","status":"enabled"}`, 200, false},
		{"denied", "v1", `{"kind":"iam","status":"enabled"}`, 403, false},
		{"unauthorized", "v1", `{"kind":"iam","status":"enabled"}`, 401, false},
		{"legacy-404", "", `not found`, 404, false},
		{"legacy-browser", "", `<html>login</html>`, 200, false},
		{"malformed", "v1", `{`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/otterio/admin/v3/self-credentials" || !strings.Contains(r.Header.Get("Authorization"), "Credential=native-user/") {
					t.Error("authentication changed identity or required storage listing")
				}
				if tc.header != "" {
					w.Header().Set(selfCapabilityHeader, tc.header)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer upstream.Close()
			client := testClient(t, Config{S3URL: upstream.URL, AccessKey: "native-user", SecretKey: "native-secret"})
			err := client.AuthenticateNative(context.Background())
			if (err == nil) != tc.allowed || calls != 1 {
				t.Fatalf("unexpected authentication: allowed=%v err=%v calls=%d", tc.allowed, err, calls)
			}
		})
	}
}
