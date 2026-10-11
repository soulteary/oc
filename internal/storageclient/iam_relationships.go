// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/otterio/pkg/madmin"
)

var _ consoleapi.IAMRelationshipsBackend = (*Client)(nil)

func iamRelationshipsUnknown() *consoleapi.Error {
	return &consoleapi.Error{Status: 501, Code: "identity_relationships_unknown", Message: "The server cannot establish authoritative native identity relationships. This snapshot cannot authorize an IAM change."}
}
func (c *Client) IAMRelationships(ctx context.Context, kind, target string) (consoleapi.IAMRelationships, error) {
	var result consoleapi.IAMRelationships
	if !consoleapi.ValidIAMName(target) || (kind != "user" && kind != "group" && kind != "service-account") {
		return result, invalidRequest()
	}
	resp, err := c.nativeIAMRequest(ctx, "iam-relationships", http.MethodGet, kind, target, nil)
	if err != nil {
		return result, iamError(err)
	}
	if singleProtocolHeader(resp.Header, "X-Otterio-IAM-Relationships") != "v1" && (resp.StatusCode == 200 || resp.StatusCode == 400 || resp.StatusCode == 404 || resp.StatusCode == 501) {
		resp.Body.Close()
		return result, iamRelationshipsUnknown()
	}
	data, err := readBounded(resp.Body, maxManagementReply)
	if err != nil {
		return result, iamRelationshipsUnknown()
	}
	if resp.StatusCode != 200 {
		code, err := managementErrorCode(data, true)
		if err != nil {
			return result, iamRelationshipsUnknown()
		}
		return result, iamError(madmin.ErrorResponse{Code: code})
	}
	result, err = decodeIAMRelationships(data, resp.Header, kind, target, time.Now().UTC())
	if err != nil {
		return consoleapi.IAMRelationships{}, err
	}
	result.StorageTarget = c.IAMStorageTarget()
	return result, nil
}
func decodeIAMRelationships(data []byte, header http.Header, kind, target string, now time.Time) (consoleapi.IAMRelationships, error) {
	var result consoleapi.IAMRelationships
	fields, err := managementJSONObject(data)
	if err != nil {
		return result, iamRelationshipsUnknown()
	}
	for _, field := range []struct {
		name        string
		destination any
	}{
		{"kind", &result.Kind}, {"target", &result.Target}, {"state", &result.State}, {"revision", &result.Revision}, {"expiresAt", &result.ExpiresAt}, {"affectedAccessKeys", &result.AffectedAccessKeys},
	} {
		if json.Unmarshal(fields[field.name], field.destination) != nil {
			return result, iamRelationshipsUnknown()
		}
	}
	if capability, ok := fields["coupledUserSecret"]; ok {
		if json.Unmarshal(capability, &result.CoupledUserSecret) != nil {
			return result, iamRelationshipsUnknown()
		}
	}
	if capability, ok := fields["coupledBindings"]; ok {
		if json.Unmarshal(capability, &result.CoupledBindings) != nil {
			return result, iamRelationshipsUnknown()
		}
	}
	if parent, ok := fields["nativeParent"]; ok {
		if json.Unmarshal(parent, &result.NativeParent) != nil {
			return result, iamRelationshipsUnknown()
		}
	}
	if result.Kind != kind || result.Target != target || result.State != "known" || !validRevision(result.Revision) || result.AffectedAccessKeys == nil || len(result.AffectedAccessKeys) > 4096 || !result.ExpiresAt.After(now) || result.ExpiresAt.After(now.Add(time.Minute)) || singleProtocolHeader(header, "X-Otterio-IAM-Relationships") != "v1" || singleProtocolHeader(header, "X-Otterio-IAM-Impact-Revision") != result.Revision {
		return result, iamRelationshipsUnknown()
	}
	if (kind == "service-account" && !consoleapi.ValidIAMAccessKey(result.NativeParent)) || (kind != "service-account" && result.NativeParent != "") {
		return result, iamRelationshipsUnknown()
	}
	seen := map[string]bool{}
	for _, key := range result.AffectedAccessKeys {
		if !consoleapi.ValidIAMAccessKey(key) || seen[key] {
			return result, iamRelationshipsUnknown()
		}
		seen[key] = true
	}
	if (kind == "user" || kind == "service-account") && !seen[target] {
		return result, iamRelationshipsUnknown()
	}
	return result, nil
}

func (c *Client) IAMStorageTarget() string {
	scheme := "http://"
	if c.s3Options.Secure {
		scheme = "https://"
	}
	return scheme + c.s3Endpoint + "\x00" + c.adminEndpoint
}
