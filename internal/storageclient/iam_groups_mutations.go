// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
)

var _ consoleapi.IAMRelationshipGroupsBackend = (*Client)(nil)

func validConditionalGroupAction(args consoleapi.IAMActionRequest) bool {
	switch args.Action {
	case "group.add-members", "group.remove-members", "group.enable", "group.disable", "group.delete":
		return consoleapi.ValidIAMAction(args)
	}
	return false
}
func sameGroupImpact(a, b []string) bool {
	if a == nil || b == nil || len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, key := range a {
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	for _, key := range b {
		if !seen[key] {
			return false
		}
		delete(seen, key)
	}
	return len(seen) == 0
}

func (c *Client) IAMMutateRelationshipGroup(ctx context.Context, args consoleapi.IAMActionRequest, relation consoleapi.IAMRelationships) (consoleapi.IAMGroupMutationResult, error) {
	var result consoleapi.IAMGroupMutationResult
	scheme := "http://"
	if c.s3Options.Secure {
		scheme = "https://"
	}
	target := scheme + c.s3Endpoint + "\x00" + c.adminEndpoint
	if !validConditionalGroupAction(args) {
		return result, invalidRequest()
	}
	if !relation.CoupledGroups || relation.Kind != "group" || relation.Target != args.Group || relation.StorageTarget != target {
		return result, iamRelationshipsUnknown()
	}
	data, _ := json.Marshal(relation)
	headers := http.Header{}
	headers.Set("X-Otterio-IAM-Relationships", "v1")
	headers.Set("X-Otterio-IAM-Impact-Revision", relation.Revision)
	if _, err := decodeIAMRelationships(data, headers, "group", args.Group, time.Now().UTC()); err != nil {
		return result, err
	}
	data, err := json.Marshal(struct {
		Action  string   `json:"action"`
		Target  string   `json:"target"`
		Members []string `json:"members"`
	}{args.Action, args.Group, append([]string{}, args.Members...)})
	if err != nil {
		return result, invalidRequest()
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil || credential.SignerType != credentials.SignatureV4 {
		return result, iamUnsupported()
	}
	endpoint, err := url.Parse(c.adminEndpoint + "/otterio/admin/v3/iam-relationship-groups")
	if err != nil {
		return result, configError()
	}
	endpoint.RawQuery = url.Values{"kind": {"group"}, "target": {args.Group}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return result, invalidRequest()
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", c.appName+"/"+c.appVersion)
	request.Header.Set("X-Otterio-IAM-Impact-If-Match", relation.Revision)
	request.Header.Set("X-Otterio-IAM-Impact-Expires", relation.ExpiresAt.UTC().Format(time.RFC3339Nano))
	setPayloadHash(request, data)
	request = signer.SignV4(*request, credential.AccessKeyID, credential.SecretAccessKey, credential.SessionToken, "")
	response, err := c.adminTransport.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return result, outcomeUnknown()
	}
	body, err := readBounded(response.Body, maxManagementReply)
	if err != nil {
		return result, outcomeUnknown()
	}
	if response.StatusCode == 412 {
		code, parseErr := managementErrorCode(body, true)
		if parseErr == nil && code == "PreconditionFailed" {
			return result, iamConflict("identity_revision_conflict", "The IAM graph changed. Reload before changing this group.")
		}
	}
	observer := iamTransport{dispatched: true, status: response.StatusCode, data: body, allowBody: true}
	if err := observer.mutationError(nil); err != nil {
		return result, err
	}
	if response.StatusCode != 200 {
		return result, outcomeUnknown()
	}
	fields, err := managementJSONObject(body)
	if err != nil {
		return result, outcomeUnknown()
	}
	if json.Unmarshal(fields["outcome"], &result.Outcome) != nil || result.Outcome != "confirmed" {
		return result, outcomeUnknown()
	}
	groupFields, err := managementJSONObject(fields["group"])
	if err != nil {
		return result, outcomeUnknown()
	}
	for _, field := range []struct {
		name        string
		destination any
	}{{"name", &result.Group.Name}, {"status", &result.Group.Status}, {"members", &result.Group.Members}, {"policies", &result.Group.Policies}} {
		if json.Unmarshal(groupFields[field.name], field.destination) != nil {
			return result, outcomeUnknown()
		}
	}
	if result.Group.Name != args.Group || result.Group.Members == nil || result.Group.Policies == nil || len(result.Group.Members) > 4096 {
		return result, outcomeUnknown()
	}
	seen := map[string]bool{}
	for _, member := range result.Group.Members {
		if !consoleapi.ValidIAMAccessKey(member) || seen[member] {
			return result, outcomeUnknown()
		}
		seen[member] = true
	}
	if args.Action == "group.delete" {
		if result.Group.Status != "deleted" || len(result.Group.Members) != 0 {
			return result, outcomeUnknown()
		}
	} else if result.Group.Status != "enabled" && result.Group.Status != "disabled" {
		return result, outcomeUnknown()
	}
	if args.Action == "group.enable" && result.Group.Status != "enabled" || args.Action == "group.disable" && result.Group.Status != "disabled" {
		return result, outcomeUnknown()
	}
	for _, member := range args.Members {
		if args.Action == "group.add-members" && !seen[member] || args.Action == "group.remove-members" && seen[member] {
			return result, outcomeUnknown()
		}
	}
	result.Impact, err = decodeIAMRelationships(fields["impact"], response.Header, "group", args.Group, time.Now().UTC())
	// Existing shared decoder may not yet include this additive capability.
	impactFields, fieldsErr := managementJSONObject(fields["impact"])
	if fieldsErr != nil || json.Unmarshal(impactFields["coupledGroups"], &result.Impact.CoupledGroups) != nil || err != nil || !result.Impact.CoupledGroups || !sameGroupImpact(relation.AffectedAccessKeys, result.Impact.AffectedAccessKeys) {
		return result, outcomeUnknown()
	}
	result.Impact.StorageTarget = target
	return result, nil
}
