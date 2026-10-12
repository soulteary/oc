// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/soulteary/mc/internal/consoleapi"
)

func TestIAMMutationLockedAuthorizationDenialIsConclusive(t *testing.T) {
	for _, code := range []string{"XOtterioIAMActionNotAllowed", "XMinioIAMActionNotAllowed"} {
		data, _ := json.Marshal(map[string]string{"Code": code, "Message": "operation not allowed"})
		observer := iamTransport{dispatched: true, status: 403, data: data, allowBody: true}
		var result *consoleapi.Error
		if !errors.As(observer.mutationError(nil), &result) || result.Status != 403 || result.Code != "iam_action_denied" {
			t.Fatal("locked authorization rejection became an uncertain write")
		}
		observer.status = 503
		if !errors.As(observer.mutationError(nil), &result) || result.Code != "outcome_unknown" {
			t.Fatal("server failure became a conclusive rejection")
		}
	}
	observer := iamTransport{dispatched: true, status: 403, data: []byte(`{"Code":"UnrecognizedIAMDenial","Message":"unknown"}`), allowBody: true}
	var result *consoleapi.Error
	if !errors.As(observer.mutationError(nil), &result) || result.Code != "outcome_unknown" {
		t.Fatal("unknown error code became conclusive")
	}
}
