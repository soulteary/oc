// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package consoleapi

import "context"

// SettingsBackend is optional and always uses the startup-selected identity.
// Config revisions are server-issued and protect full-document replacement.
// Rotation changes only the selected IAM user's secret; credentials are never
// returned, and callers must retire the old connection after an uncertain write.
type SettingsBackend interface {
	BucketSetting(ctx context.Context, bucket, kind string) (BucketSetting, error)
	SaveBucketSetting(ctx context.Context, bucket, kind, document, revision string, remove bool) (BucketSetting, error)
	SelfAccount(context.Context) (SelfAccount, error)
	// A typed Error other than outcome_unknown asserts a known rejection.
	// After dispatch, ambiguous responses must use outcome_unknown (or an
	// untyped error), so callers do not retain potentially stale credentials.
	RotateOwnSecret(ctx context.Context, newSecret string) error
}

type BucketSetting struct {
	Bucket      string `json:"bucket"`
	Kind        string `json:"kind"`
	Format      string `json:"format"`
	Document    string `json:"document"`
	Revision    string `json:"revision"`
	Exists      bool   `json:"exists"`
	Conditional bool   `json:"conditional"`
}

// SelfAccount contains no access key, secret key, token or full IAM policy.
type SelfAccount struct {
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	CanRotateSecret bool   `json:"canRotateSecret"`
}
