package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soulteary/mc/pkg/probe"
)

type retentionCopyProgressReader struct {
	reads int
}

func (r *retentionCopyProgressReader) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("copy progress was read before validating the retention date")
}

func TestUploadSourceToTargetURLRejectsUnrepresentableRetention(t *testing.T) {
	// Keep the local copy fixture independent of the user's alias configuration.
	previousLoader := loadMcConfig
	loadMcConfig = func() (*configV10, *probe.Error) { return newConfigV10(), nil }
	t.Cleanup(func() { loadMcConfig = previousLoader })

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source")
	targetPath := filepath.Join(dir, "target")
	data := []byte("retention must be validated before copying this data")
	if err := os.WriteFile(sourcePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	urls := URLs{
		SourceContent: &ClientContent{
			URL:  *newClientURL(sourcePath),
			Size: int64(len(data)),
		},
		TargetContent: &ClientContent{
			URL:               *newClientURL(targetPath),
			RetentionEnabled:  true,
			RetentionMode:     "GOVERNANCE",
			RetentionDuration: "2147483647y",
		},
	}
	progress := &retentionCopyProgressReader{}
	result := uploadSourceToTargetURL(context.Background(), urls, progress, nil, false)
	if result.Error == nil {
		t.Fatal("upload accepted a retention date outside the RFC3339 year range")
	}
	if cause := result.Error.ToGoError(); cause == nil || !strings.Contains(cause.Error(), "invalid validity") {
		t.Errorf("upload error = %v; want invalid validity", cause)
	}
	if progress.reads != 0 {
		t.Errorf("copy progress was read %d times before rejecting retention; want 0", progress.reads)
	}
	if _, err := os.Stat(targetPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("target exists or could not be checked after retention rejection: %v", err)
	}
}
