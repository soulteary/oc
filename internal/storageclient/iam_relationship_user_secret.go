// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package storageclient

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/soulteary/mc/internal/consoleapi"
)

var _ consoleapi.IAMRelationshipSecretsBackend = (*Client)(nil)

func (c *Client) IAMRotateRelationshipUserSecret(ctx context.Context, binding consoleapi.IAMBindings, relation consoleapi.IAMRelationships, secret string) (consoleapi.IAMRelationshipMutationResult, error) {
	var result consoleapi.IAMRelationshipMutationResult
	if !relation.CoupledUserSecret || binding.Kind != "user" || binding.Policies == nil {
		return result, iamRelationshipsUnknown()
	}
	if len(secret) < 8 || len(secret) > 128 || !utf8.ValidString(secret) || strings.ContainsAny(secret, "\x00\r\n") {
		return result, invalidRequest()
	}
	data, err := json.Marshal(struct {
		NewSecretKey string `json:"newSecretKey"`
	}{secret})
	if err != nil {
		return result, invalidRequest()
	}
	return c.dispatchRelationshipMutation(ctx, "iam-relationship-user-secret", binding, relation, data, binding.Policies, true)
}
