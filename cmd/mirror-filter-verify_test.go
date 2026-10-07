package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/soulteary/mc/pkg/probe"
)

func TestMirrorFiltersBeforeContentVerification(t *testing.T) {
	previous := loadMcConfig
	loadMcConfig = func() (*configV10, *probe.Error) { return newConfigV10(), nil }
	defer func() { loadMcConfig = previous }()
	for _, kind := range []string{"older-than", "newer-than"} {
		t.Run(kind, func(t *testing.T) {
			source, target := t.TempDir(), t.TempDir()
			a, b := filepath.Join(source, "file"), filepath.Join(target, "file")
			for _, path := range []string{a, b} {
				if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(b, 0); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(b, 0600)
			opts := mirrorOptions{isOverwrite: true, verifyContents: true}
			timestamp := time.Now()
			if kind == "older-than" {
				opts.olderThan = "1d"
			} else {
				opts.newerThan = "1d"
				timestamp = timestamp.Add(-48 * time.Hour)
			}
			for _, path := range []string{a, b} {
				if err := os.Chtimes(path, timestamp, timestamp); err != nil {
					t.Fatal(err)
				}
			}
			for result := range prepareMirrorURLs(context.Background(), source, target, opts) {
				if result.Error != nil || result.SourceContent != nil {
					t.Fatalf("excluded file produced work: %+v", result)
				}
			}
		})
	}
}
