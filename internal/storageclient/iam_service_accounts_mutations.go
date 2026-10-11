// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

var _ consoleapi.IAMRelationshipServiceAccountsBackend = (*Client)(nil)

func validConditionalServiceAccountAction(args consoleapi.IAMActionRequest) bool {
	switch args.Action {
	case "service-account.enable", "service-account.disable", "service-account.rotate", "service-account.policy", "service-account.delete":
	default:
		return false
	}
	if !consoleapi.ValidIAMAction(args) || len(args.Policy) > 16<<10 {
		return false
	}
	if args.Action == "service-account.policy" {
		_, err := iampolicy.ParseConfig(strings.NewReader(args.Policy))
		return err == nil
	}
	return true
}
func (c *Client) IAMMutateRelationshipServiceAccount(ctx context.Context, args consoleapi.IAMActionRequest, relation consoleapi.IAMRelationships) (consoleapi.IAMServiceAccountMutationResult, error) {
	var result consoleapi.IAMServiceAccountMutationResult
	target := c.IAMStorageTarget()
	if !validConditionalServiceAccountAction(args) {
		return result, invalidRequest()
	}
	if relation.Kind != "service-account" || relation.Target != args.AccessKey || !relation.CoupledServiceAccounts || relation.StorageTarget != target || !consoleapi.ValidIAMAccessKey(relation.NativeParent) || relation.NativeParent == args.AccessKey || len(relation.AffectedAccessKeys) != 1 || relation.AffectedAccessKeys[0] != args.AccessKey {
		return result, iamRelationshipsUnknown()
	}
	data, _ := json.Marshal(relation)
	headers := http.Header{}
	headers.Set("X-Otterio-IAM-Relationships", "v1")
	headers.Set("X-Otterio-IAM-Impact-Revision", relation.Revision)
	if _, err := decodeIAMRelationships(data, headers, "service-account", args.AccessKey, time.Now().UTC()); err != nil {
		return result, err
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil || credential.SignerType != credentials.SignatureV4 {
		return result, iamUnsupported()
	}
	plaintext, err := json.Marshal(struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Secret string `json:"newSecretKey,omitempty"`
		Policy string `json:"policy,omitempty"`
	}{args.Action, args.AccessKey, args.SecretKey, args.Policy})
	if err != nil {
		return result, invalidRequest()
	}
	encrypted, err := madmin.EncryptData(credential.SecretAccessKey, plaintext)
	args.SecretKey = ""
	clear(plaintext)
	if err != nil {
		return result, invalidRequest()
	}
	endpoint, err := url.Parse(c.adminEndpoint + "/otterio/admin/v3/iam-relationship-service-accounts")
	if err != nil {
		return result, configError()
	}
	endpoint.RawQuery = url.Values{"kind": {"service-account"}, "target": {args.AccessKey}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), bytes.NewReader(encrypted))
	if err != nil {
		return result, invalidRequest()
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("User-Agent", c.appName+"/"+c.appVersion)
	request.Header.Set("X-Otterio-IAM-Impact-If-Match", relation.Revision)
	request.Header.Set("X-Otterio-IAM-Impact-Expires", relation.ExpiresAt.UTC().Format(time.RFC3339Nano))
	setPayloadHash(request, encrypted)
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
			return result, iamConflict("identity_revision_conflict", "The service account or its parent changed. Reload before another change.")
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
	accountFields, err := managementJSONObject(fields["serviceAccount"])
	if err != nil {
		return result, outcomeUnknown()
	}
	for _, field := range []struct {
		name        string
		destination any
	}{{"accessKey", &result.ServiceAccount.AccessKey}, {"parentUser", &result.ServiceAccount.ParentUser}, {"status", &result.ServiceAccount.Status}, {"impliedPolicy", &result.ServiceAccount.ImpliedPolicy}, {"policy", &result.ServiceAccount.Policy}} {
		if json.Unmarshal(accountFields[field.name], field.destination) != nil {
			return result, outcomeUnknown()
		}
	}
	if result.ServiceAccount.AccessKey != args.AccessKey || result.ServiceAccount.ParentUser != relation.NativeParent {
		return result, outcomeUnknown()
	}
	if args.Action == "service-account.delete" {
		if result.ServiceAccount.Status != "deleted" {
			return result, outcomeUnknown()
		}
	} else if result.ServiceAccount.Status != "on" && result.ServiceAccount.Status != "off" {
		return result, outcomeUnknown()
	}
	if args.Action == "service-account.enable" && result.ServiceAccount.Status != "on" || args.Action == "service-account.disable" && result.ServiceAccount.Status != "off" {
		return result, outcomeUnknown()
	}
	if args.Action == "service-account.policy" {
		wanted, parseErr := iampolicy.ParseConfig(strings.NewReader(args.Policy))
		actual, actualErr := iampolicy.ParseConfig(strings.NewReader(result.ServiceAccount.Policy))
		if parseErr != nil || actualErr != nil || result.ServiceAccount.ImpliedPolicy || !reflect.DeepEqual(wanted, actual) {
			return result, outcomeUnknown()
		}
	}
	result.Impact, err = decodeIAMRelationships(fields["impact"], response.Header, "service-account", args.AccessKey, time.Now().UTC())
	impactFields, fieldsErr := managementJSONObject(fields["impact"])
	if fieldsErr != nil || json.Unmarshal(impactFields["coupledServiceAccounts"], &result.Impact.CoupledServiceAccounts) != nil || err != nil || !result.Impact.CoupledServiceAccounts || result.Impact.NativeParent != relation.NativeParent || len(result.Impact.AffectedAccessKeys) != 1 || result.Impact.AffectedAccessKeys[0] != args.AccessKey {
		return result, outcomeUnknown()
	}
	result.Impact.StorageTarget = target
	return result, nil
}
