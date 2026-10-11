// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

var _ consoleapi.IAMRelationshipServiceCreationBackend = (*Client)(nil)

const nativeServiceCreationHeader = "X-Otterio-IAM-Service-Creation"

func (c *Client) IAMCreateRelationshipServiceAccount(ctx context.Context, args consoleapi.IAMActionRequest, relation consoleapi.IAMRelationships) (consoleapi.IAMActionResult, error) {
	var result consoleapi.IAMActionResult
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if args.Action != "service-account.create" || !consoleapi.ValidIAMAction(args) {
		return result, invalidRequest()
	}
	if !relation.CoupledServiceCreation || relation.Kind != "user" || relation.Target != args.User || relation.StorageTarget != c.IAMStorageTarget() {
		return result, iamUnsupported()
	}
	snapshot, _ := json.Marshal(relation)
	headers := http.Header{}
	headers.Set("X-Otterio-IAM-Relationships", "v1")
	headers.Set("X-Otterio-IAM-Impact-Revision", relation.Revision)
	if _, err := decodeIAMRelationships(snapshot, headers, "user", args.User, time.Now().UTC()); err != nil {
		return result, err
	}
	var policy *iampolicy.Policy
	if args.Policy != "" {
		var err error
		policy, err = iampolicy.ParseConfig(bytes.NewBufferString(args.Policy))
		if err != nil {
			return result, invalidRequest()
		}
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil || credential.SignerType != credentials.SignatureV4 || credential.SessionToken != "" {
		return result, iamUnsupported()
	}
	body, err := json.Marshal(madmin.AddServiceAccountReq{TargetUser: args.User, AccessKey: args.AccessKey, SecretKey: args.SecretKey, Policy: policy})
	if err != nil {
		return result, invalidRequest()
	}
	body, err = madmin.EncryptData(credential.SecretAccessKey, body)
	if err != nil || len(body) > 32*1024 {
		return result, invalidRequest()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.adminEndpoint+"/otterio/admin/v3/add-service-account", bytes.NewReader(body))
	if err != nil {
		return result, configError()
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", c.appName+"/"+c.appVersion)
	req.Header.Set(nativeServiceCreationHeader, "v1")
	req.Header.Set("X-Otterio-IAM-Impact-If-Match", relation.Revision)
	req.Header.Set("X-Otterio-IAM-Impact-Expires", relation.ExpiresAt.UTC().Format(time.RFC3339Nano))
	setPayloadHash(req, body)
	req = signer.SignV4(*req, credential.AccessKeyID, credential.SecretAccessKey, credential.SessionToken, "")
	response, err := c.adminTransport.RoundTrip(req)
	if err != nil || response == nil || response.Body == nil {
		return result, outcomeUnknown()
	}
	encrypted, err := readBounded(response.Body, 32*1024)
	if err != nil {
		return result, outcomeUnknown()
	}
	observer := iamTransport{dispatched: true, status: response.StatusCode, data: encrypted, allowBody: true}
	if err := observer.mutationError(nil); err != nil {
		return result, err
	}
	if response.StatusCode != http.StatusOK || len(response.Header.Values(nativeServiceCreationHeader)) != 1 || response.Header.Get(nativeServiceCreationHeader) != "v1" {
		return result, outcomeUnknown()
	}
	decoded, err := madmin.DecryptData(credential.SecretAccessKey, bytes.NewReader(encrypted))
	if err != nil {
		return result, outcomeUnknown()
	}
	fields, err := managementJSONObject(decoded)
	if err != nil || len(fields) != 1 || fields["credentials"] == nil {
		return result, outcomeUnknown()
	}
	credentialFields, err := managementJSONObject(fields["credentials"])
	if err != nil {
		return result, outcomeUnknown()
	}
	for field := range credentialFields {
		switch field {
		case "accessKey", "secretKey", "parentUser", "expiration", "sessionToken", "status", "groups":
		default:
			return result, outcomeUnknown()
		}
	}
	var created madmin.AddServiceAccountResp
	if json.Unmarshal(decoded, &created) != nil || !consoleapi.ValidIAMAccessKey(created.Credentials.AccessKey) || len(created.Credentials.SecretKey) < 8 || len(created.Credentials.SecretKey) > 128 || created.Credentials.ParentUser != args.User || created.Credentials.SessionToken != "" || !created.Credentials.Expiration.IsZero() || len(created.Credentials.Groups) != 0 || (created.Credentials.Status != "" && created.Credentials.Status != "on") || (args.AccessKey != "" && args.AccessKey != created.Credentials.AccessKey) || (args.SecretKey != "" && args.SecretKey != created.Credentials.SecretKey) {
		return result, outcomeUnknown()
	}
	ackArgs := args
	ackArgs.SecretKey = created.Credentials.SecretKey
	ackArgs.AccessKey = created.Credentials.AccessKey
	if !consoleapi.ValidIAMAction(ackArgs) {
		return result, outcomeUnknown()
	}
	result.Outcome = "confirmed"
	if args.SecretKey == "" {
		result.Credentials = &consoleapi.IAMCredentials{AccessKey: created.Credentials.AccessKey, SecretKey: created.Credentials.SecretKey}
	}
	return result, nil
}
