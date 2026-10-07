package cmd

import (
	"context"
	"testing"
	"time"
)

func TestMirrorPeriodicScanDoesNotAlwaysVerify(t *testing.T) {
	for _, kind := range []string{"disabled", "not-due", "due"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			job := &mirrorJob{watcher: NewWatcher(time.Now()), stopCh: make(chan struct{})}
			job.watcher.localFilesystem = true
			job.opts.watchRescanInterval = time.Millisecond
			if kind != "disabled" {
				job.opts.watchVerifyInterval = time.Hour
			}
			if kind == "not-due" {
				job.opts.nextVerify = time.Now().Add(time.Hour)
			}
			previous := job.opts.nextVerify
			job.watchMirror(ctx, func() {})
			if ctx.Err() != nil {
				t.Fatal("periodic scan did not finish")
			}
			if job.verifyContents != (kind == "due") {
				t.Fatalf("deep verification=%v", job.verifyContents)
			}
			if kind == "due" {
				if !job.opts.nextVerify.After(time.Now()) {
					t.Fatal("deep verification deadline not advanced")
				}
			} else if !job.opts.nextVerify.Equal(previous) {
				t.Fatal("pending deadline reset by metadata rescan")
			}
		})
	}
}
