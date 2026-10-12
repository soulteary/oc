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

var _ consoleapi.IAMRelationshipUsersBackend = (*Client)(nil)

func validConditionalUserAction(args consoleapi.IAMActionRequest) bool {
	switch args.Action {
	case "user.enable", "user.disable", "user.delete":
		return consoleapi.ValidIAMAction(args)
	}
	return false
}
func (c *Client) IAMMutateRelationshipUser(ctx context.Context, args consoleapi.IAMActionRequest, relation consoleapi.IAMRelationships) (consoleapi.IAMUserMutationResult, error) {
	var result consoleapi.IAMUserMutationResult
	scheme := "http://"
	if c.s3Options.Secure {
		scheme = "https://"
	}
	target := scheme + c.s3Endpoint + "\x00" + c.adminEndpoint
	if !validConditionalUserAction(args) {
		return result, invalidRequest()
	}
	if !relation.CoupledUsers || relation.Kind != "user" || relation.Target != args.User || relation.StorageTarget != target {
		return result, iamRelationshipsUnknown()
	}
	data, _ := json.Marshal(relation)
	headers := http.Header{}
	headers.Set("X-Otterio-IAM-Relationships", "v1")
	headers.Set("X-Otterio-IAM-Impact-Revision", relation.Revision)
	if _, err := decodeIAMRelationships(data, headers, "user", args.User, time.Now().UTC()); err != nil {
		return result, err
	}
	data, err := json.Marshal(struct {
		Action string `json:"action"`
		Target string `json:"target"`
	}{args.Action, args.User})
	if err != nil {
		return result, invalidRequest()
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil || credential.SignerType != credentials.SignatureV4 {
		return result, iamUnsupported()
	}
	endpoint, err := url.Parse(c.adminEndpoint + "/otterio/admin/v3/iam-relationship-users")
	if err != nil {
		return result, configError()
	}
	endpoint.RawQuery = url.Values{"kind": {"user"}, "target": {args.User}}.Encode()
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
			return result, iamConflict("identity_revision_conflict", "The IAM graph changed. Reload before changing this user.")
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
	userFields, err := managementJSONObject(fields["user"])
	if err != nil || len(userFields) != 2 || json.Unmarshal(userFields["accessKey"], &result.User.AccessKey) != nil || json.Unmarshal(userFields["status"], &result.User.Status) != nil || result.User.AccessKey != args.User {
		return result, outcomeUnknown()
	}
	expected := map[string]string{"user.enable": "enabled", "user.disable": "disabled", "user.delete": "deleted"}[args.Action]
	if result.User.Status != expected {
		return result, outcomeUnknown()
	}
	result.Impact, err = decodeIAMRelationships(fields["impact"], response.Header, "user", args.User, time.Now().UTC())
	if err != nil || !result.Impact.CoupledUsers || !sameGroupImpact(relation.AffectedAccessKeys, result.Impact.AffectedAccessKeys) {
		return result, outcomeUnknown()
	}
	result.Impact.StorageTarget = target
	return result, nil
}
