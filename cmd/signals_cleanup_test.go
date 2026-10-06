package cmd

import (
	"testing"
	"time"
)

func TestSignalCleanupWaitsForCompletion(t *testing.T) {
	done := make(chan struct{})
	returned := make(chan bool)
	go func() { returned <- awaitCommandCleanup(done, time.Second) }()
	select {
	case <-returned:
		t.Fatal("cleanup returned before completion")
	case <-time.After(20 * time.Millisecond):
	}
	close(done)
	if !<-returned {
		t.Fatal("completed cleanup rejected")
	}
	if awaitCommandCleanup(make(chan struct{}), 20*time.Millisecond) {
		t.Fatal("stuck cleanup reported success")
	}
}
