// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
)

var _ consoleapi.IAMRelationshipMutationsBackend = (*Client)(nil)

func (c *Client) IAMReplaceRelationshipBindings(ctx context.Context, binding consoleapi.IAMBindings, relation consoleapi.IAMRelationships, policies []string) (consoleapi.IAMRelationshipMutationResult, error) {
	var result consoleapi.IAMRelationshipMutationResult
	scheme := "http://"
	if c.s3Options.Secure {
		scheme = "https://"
	}
	target := scheme + c.s3Endpoint + "\x00" + c.adminEndpoint
	if !binding.Conditional || !validRevision(binding.Revision) || !relation.CoupledBindings || relation.StorageTarget != target || relation.Kind != binding.Kind || relation.Target != binding.Target || (binding.Kind != "user" && binding.Kind != "group") || !consoleapi.ValidIAMName(binding.Target) {
		return result, iamRelationshipsUnknown()
	}
	// Reuse query validation to reject null, stale, malformed or mismatched scope
	// snapshots before any network mutation. The server rechecks atomically.
	relationData, _ := json.Marshal(relation)
	header := http.Header{}
	header.Set("X-Otterio-IAM-Relationships", "v1")
	header.Set("X-Otterio-IAM-Impact-Revision", relation.Revision)
	if _, err := decodeIAMRelationships(relationData, header, binding.Kind, binding.Target, time.Now().UTC()); err != nil {
		return result, err
	}
	if len(policies) > 100 || policies == nil {
		return result, invalidRequest()
	}
	seen := map[string]bool{}
	for _, policy := range policies {
		if !consoleapi.ValidIAMName(policy) || strings.ContainsRune(policy, ',') || seen[policy] {
			return result, invalidRequest()
		}
		seen[policy] = true
	}
	data, err := json.Marshal(struct {
		Policies []string `json:"policies"`
	}{policies})
	if err != nil {
		return result, invalidRequest()
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil || credential.SignerType != credentials.SignatureV4 {
		return result, iamUnsupported()
	}
	endpoint, err := url.Parse(c.adminEndpoint + "/otterio/admin/v3/iam-relationship-bindings")
	if err != nil {
		return result, configError()
	}
	endpoint.RawQuery = url.Values{"kind": {binding.Kind}, "target": {binding.Target}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return result, invalidRequest()
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", c.appName+"/"+c.appVersion)
	request.Header.Set(iamBindingsCondition, binding.Revision)
	request.Header.Set("X-Otterio-IAM-Impact-If-Match", relation.Revision)
	request.Header.Set("X-Otterio-IAM-Impact-Expires", relation.ExpiresAt.UTC().Format(time.RFC3339Nano))
	setPayloadHash(request, data)
	request = signer.SignV4(*request, credential.AccessKeyID, credential.SecretAccessKey, credential.SessionToken, "")
	// One dispatch only: do not replay an IAM mutation after an uncertain result.
	response, err := c.adminTransport.RoundTrip(request)
	if err != nil {
		return result, outcomeUnknown()
	}
	body, err := readBounded(response.Body, maxManagementReply)
	if err != nil {
		return result, outcomeUnknown()
	}
	if response.StatusCode == 412 {
		code, parseErr := managementErrorCode(body, true)
		if parseErr == nil && code == "PreconditionFailed" {
			return result, iamConflict("identity_revision_conflict", "The identity graph or policy bindings changed. Reload the target before making another change.")
		}
	}
	observer := iamTransport{dispatched: true, status: response.StatusCode, data: body, allowBody: true}
	if err := observer.mutationError(nil); err != nil {
		return result, err
	}
	if response.StatusCode != 200 || singleProtocolHeader(response.Header, iamBindingsCapability) != "v1" {
		return result, outcomeUnknown()
	}
	fields, err := managementJSONObject(body)
	if err != nil {
		return result, outcomeUnknown()
	}
	if json.Unmarshal(fields["outcome"], &result.Outcome) != nil || result.Outcome != "confirmed" {
		return result, outcomeUnknown()
	}
	bindingFields, err := managementJSONObject(fields["bindings"])
	if err != nil {
		return result, outcomeUnknown()
	}
	for _, field := range []struct {
		name        string
		destination any
	}{{"kind", &result.Bindings.Kind}, {"target", &result.Bindings.Target}, {"conditional", &result.Bindings.Conditional}, {"revision", &result.Bindings.Revision}, {"policies", &result.Bindings.Policies}} {
		if json.Unmarshal(bindingFields[field.name], field.destination) != nil {
			return result, outcomeUnknown()
		}
	}
	expectedPolicies := append([]string{}, policies...)
	actualPolicies := append([]string{}, result.Bindings.Policies...)
	sort.Strings(expectedPolicies)
	sort.Strings(actualPolicies)
	if len(expectedPolicies) != len(actualPolicies) {
		return result, outcomeUnknown()
	}
	for i := range expectedPolicies {
		if expectedPolicies[i] != actualPolicies[i] {
			return result, outcomeUnknown()
		}
	}
	if result.Bindings.Kind != binding.Kind || result.Bindings.Target != binding.Target || !result.Bindings.Conditional || !validRevision(result.Bindings.Revision) || result.Bindings.Policies == nil || singleProtocolHeader(response.Header, iamBindingsRevision) != result.Bindings.Revision {
		return result, outcomeUnknown()
	}
	result.Impact, err = decodeIAMRelationships(fields["impact"], response.Header, binding.Kind, binding.Target, time.Now().UTC())
	if err != nil || !result.Impact.CoupledBindings {
		return result, outcomeUnknown()
	}
	// A binding replacement cannot change membership. Refuse acknowledgements
	// claiming a different affected set rather than under-retiring a principal.
	expected := map[string]bool{}
	for _, key := range relation.AffectedAccessKeys {
		expected[key] = true
	}
	if len(result.Impact.AffectedAccessKeys) != len(expected) {
		return result, outcomeUnknown()
	}
	for _, key := range result.Impact.AffectedAccessKeys {
		if !expected[key] {
			return result, outcomeUnknown()
		}
	}
	result.Impact.StorageTarget = target
	return result, nil
}
