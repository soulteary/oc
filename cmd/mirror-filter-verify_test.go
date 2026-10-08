package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio-sdk/v7/pkg/notification"
)

func TestMirrorWatchFiltersUseSourceModificationTime(t *testing.T) {
	previous := loadMcConfig
	loadMcConfig = func() (*configV10, *probe.Error) { return newConfigV10(), nil }
	defer func() { loadMcConfig = previous }()
	for _, tc := range []struct {
		name, olderThan, newerThan string
		age                        time.Duration
		want                       int
	}{
		{"older-than-excludes-new", "1d", "", 0, 0},
		{"older-than-keeps-old", "1d", "", 48 * time.Hour, 1},
		{"newer-than-keeps-new", "", "1d", 0, 1},
		{"newer-than-excludes-old", "", "1d", 48 * time.Hour, 0},
	} {
		for _, known := range []bool{false, true} {
			t.Run(tc.name+"/known="+fmt.Sprint(known), func(t *testing.T) {
				source, target := t.TempDir(), t.TempDir()
				path := filepath.Join(source, "file")
				if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
					t.Fatal(err)
				}
				modified := time.Now().Add(-tc.age)
				if err := os.Chtimes(path, modified, modified); err != nil {
					t.Fatal(err)
				}
				event := EventInfo{Path: path, Time: time.Now().Format(time.RFC3339Nano), Type: notification.ObjectCreatedPut}
				if known {
					event.SourceModTime = modified
				}
				job := &mirrorJob{
					sourceURL: source, targetURL: target,
					opts:     mirrorOptions{olderThan: tc.olderThan, newerThan: tc.newerThan},
					parallel: &ParallelManager{queueCh: make(chan task, 1)},
				}
				job.watchMirrorEvents(context.Background(), []EventInfo{event})
				if got := len(job.parallel.queueCh); got != tc.want {
					t.Fatalf("queued %d objects, want %d", got, tc.want)
				}
			})
		}
	}
}

func TestMirrorWatchExcludeMatchesScanPaths(t *testing.T) {
	previous := loadMcConfig
	loadMcConfig = func() (*configV10, *probe.Error) { return newConfigV10(), nil }
	defer func() { loadMcConfig = previous }()
	for _, tc := range []struct{ pattern, path string }{
		{"file", "file"},
		{filepath.Join("sub", "*"), filepath.Join("sub", "file")},
	} {
		for _, trailing := range []bool{false, true} {
			for _, eventType := range []notification.EventType{notification.ObjectCreatedPut, notification.ObjectRemovedDelete} {
				t.Run(tc.pattern+"/trailing="+fmt.Sprint(trailing)+"/"+string(eventType), func(t *testing.T) {
					source, target := t.TempDir(), t.TempDir()
					event := EventInfo{Path: filepath.Join(source, tc.path), Type: eventType, SourceModTime: time.Now()}
					if trailing {
						source += string(filepath.Separator)
					}
					job := &mirrorJob{
						sourceURL: source, targetURL: target,
						opts:     mirrorOptions{excludeOptions: []string{tc.pattern}, isRemove: true},
						parallel: &ParallelManager{queueCh: make(chan task, 1)},
					}
					job.status = NewQuietStatus(job.parallel)
					job.watchMirrorEvents(context.Background(), []EventInfo{event})
					if got := len(job.parallel.queueCh); got != 0 {
						t.Fatalf("excluded path queued %d operations", got)
					}
				})
			}
		}
	}
}

type mirrorWatchStatClient struct {
	Client
	modified time.Time
}

func (c mirrorWatchStatClient) Stat(context.Context, StatOptions) (*ClientContent, *probe.Error) {
	return &ClientContent{Time: c.modified}, nil
}

func TestMirrorWatchRemoteAgeUsesObjectStat(t *testing.T) {
	previousConfig, previousFactory := loadMcConfig, S3New
	defer func() { loadMcConfig, S3New = previousConfig, previousFactory }()
	loadMcConfig = func() (*configV10, *probe.Error) {
		return &configV10{Version: "10", Aliases: map[string]aliasConfigV10{
			"mirrorage": {URL: "http://example.invalid:9000", API: "S3v4", Path: "on"},
		}}, nil
	}
	for _, eventType := range []notification.EventType{notification.ObjectCreatedPut, "s3:ObjectCreated:PutRetention", "s3:ObjectCreated:PutLegalHold"} {
		for _, keepOld := range []bool{false, true} {
			t.Run(string(eventType)+"/old="+fmt.Sprint(keepOld), func(t *testing.T) {
				calls := 0
				S3New = func(config *Config) (Client, *probe.Error) {
					calls++
					if config.HostURL != "http://example.invalid:9000/bucket/file" {
						t.Errorf("stat target: %s", config.HostURL)
					}
					return mirrorWatchStatClient{modified: time.Now().Add(-48 * time.Hour)}, nil
				}
				job := &mirrorJob{
					sourceURL: "mirrorage/bucket", targetURL: t.TempDir(),
					parallel: &ParallelManager{queueCh: make(chan task, 1)},
				}
				want := 0
				if keepOld {
					job.opts.olderThan, want = "1d", 1
				} else {
					job.opts.newerThan = "1d"
				}
				job.watchMirrorEvents(context.Background(), []EventInfo{{
					Path: "http://example.invalid:9000/bucket/file", Type: eventType, Time: time.Now().Format(time.RFC3339Nano),
				}})
				if calls != 1 || len(job.parallel.queueCh) != want {
					t.Fatalf("object metadata lookups=%d, queued=%d, want=%d", calls, len(job.parallel.queueCh), want)
				}
			})
		}
	}
}

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
