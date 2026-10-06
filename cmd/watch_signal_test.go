//go:build !windows

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestWatchSignalExitCLI(t *testing.T) {
	for _, tc := range []struct {
		name   string
		signal os.Signal
		code   int
	}{
		{"interrupt", os.Interrupt, globalCancelExitStatus},
		{"terminate", syscall.SIGTERM, globalTerminatExitStatus},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ready, released := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("location") {
					fmt.Fprint(w, "<LocationConstraint>us-east-1</LocationConstraint>")
					return
				}
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				close(ready)
				<-r.Context().Done()
				close(released)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := newWatchErrorCommand(t, ctx, server.URL, false)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer command.Process.Kill()
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			select {
			case <-ready:
			case err := <-done:
				t.Fatalf("watch exited before subscription: %v", err)
			case <-ctx.Done():
				t.Fatal("watch did not subscribe")
			}
			if err := command.Process.Signal(tc.signal); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != tc.code {
					t.Fatalf("expected %d, got %v: %s %s", tc.code, err, stdout.String(), stderr.String())
				}
			case <-ctx.Done():
				t.Fatal("watch did not exit after signal")
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("signal cancellation reported an error: %s %s", stdout.String(), stderr.String())
			}
			select {
			case <-released:
			case <-time.After(time.Second):
				t.Fatal("subscription remained open")
			}
		})
	}
}
