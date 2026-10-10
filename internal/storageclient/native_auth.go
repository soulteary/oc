// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"

	"github.com/soulteary/mc/internal/consoleapi"
)

// AuthenticateNative requires the signed v1 self-credentials endpoint to
// positively confirm an enabled native IAM user. Discovery's legacy unknown
// result must never be interpreted as successful authentication.
func (c *Client) AuthenticateNative(ctx context.Context) error {
	account, err := c.SelfAccount(ctx)
	if err != nil {
		return err
	}
	if account.Kind == "unknown" || account.Status == "unknown" {
		return &consoleapi.Error{Status: 503, Code: "native_auth_unsupported", Message: "The storage server must support native identity verification."}
	}
	if account.Kind != "iam" || account.Status != "enabled" {
		return &consoleapi.Error{Status: 401, Code: "AccessDenied", Message: "Sign in with an enabled native IAM user."}
	}
	return nil
}
