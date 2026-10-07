package cmd

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/soulteary/mc/pkg/probe"
)

const fsListArg = "--oc-internal-fs-list"

type fsListRequest struct {
	URL     ClientURL
	Options ListOptions
}

// Gob preserves non-UTF-8 filesystem names, unlike JSON. Errors travel as data
// so the parent's callers retain their existing typed-error behavior.
type fsListRecord struct {
	Content ClientContent
	Error   *fsListError
	Done    bool
}

type fsListError struct {
	Kind, Message, Path, Op string
	Errno                   syscall.Errno
}

func encodeFSListError(err error) *fsListError {
	w := &fsListError{Message: err.Error()}
	switch e := err.(type) {
	case PathNotFound:
		w.Kind, w.Path = "not-found", e.Path
	case PathInsufficientPermission:
		w.Kind, w.Path = "permission", e.Path
	case TooManyLevelsSymlink:
		w.Kind, w.Path = "symlink", e.Path
	case *os.PathError:
		w.Kind, w.Path, w.Op = "path", e.Path, e.Op
		if errno, ok := e.Err.(syscall.Errno); ok {
			w.Errno = errno
		}
	}
	return w
}

func (w *fsListError) decode() error {
	switch w.Kind {
	case "not-found":
		return PathNotFound{Path: w.Path}
	case "permission":
		return PathInsufficientPermission{Path: w.Path}
	case "symlink":
		return TooManyLevelsSymlink{Path: w.Path}
	case "path":
		if w.Errno != 0 {
			return &os.PathError{Op: w.Op, Path: w.Path, Err: w.Errno}
		}
	}
	return errors.New(w.Message)
}

func init() {
	if len(os.Args) != 2 || os.Args[1] != fsListArg {
		return
	}
	var request fsListRequest
	if gob.NewDecoder(os.Stdin).Decode(&request) != nil {
		os.Exit(1)
	}
	// Losing the parent closes stdin even while a filesystem syscall is stuck.
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); os.Exit(1) }()
	client := &fsClient{PathURL: &request.URL}
	encoder := gob.NewEncoder(os.Stdout)
	for content := range client.listInProcess(context.Background(), request.Options) {
		record := fsListRecord{Content: *content}
		if content.Err != nil {
			record.Error = encodeFSListError(content.Err.ToGoError())
			record.Content.Err = nil
		}
		if encoder.Encode(record) != nil {
			os.Exit(1)
		}
	}
	if encoder.Encode(fsListRecord{Done: true}) != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func (f *fsClient) listIsolated(ctx context.Context, opts ListOptions) <-chan *ClientContent {
	out := make(chan *ClientContent)
	go func() {
		defer close(out)
		if ctx.Err() != nil {
			return
		}
		executable, err := os.Executable()
		if err == nil {
			command := exec.Command(executable, fsListArg)
			err = runFSListProcess(ctx, command, fsListRequest{URL: *f.PathURL, Options: opts}, out)
		}
		if err != nil && ctx.Err() == nil {
			sendFSListContent(ctx, out, &ClientContent{Err: probe.NewError(err)})
		}
	}()
	return out
}

func runFSListProcess(ctx context.Context, command *exec.Cmd, request fsListRequest, out chan<- *ClientContent) (result error) {
	input, err := command.StdinPipe()
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	defer output.Close()
	if err = command.Start(); err != nil {
		return err
	}
	// Wait only after the decoder stops reading: Cmd.Wait closes StdoutPipe.
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(canceled)
		_ = input.Close()
		_ = output.Close()
		_ = command.Process.Kill()
	})
	complete := false
	defer func() {
		if !stop() {
			<-canceled
		}
		if !complete || ctx.Err() != nil {
			_ = command.Process.Kill()
			_ = input.Close()
		}
		_ = output.Close()
		waited := make(chan error, 1)
		go func() { waited <- command.Wait() }()
		waitLimit := time.Second
		if complete && ctx.Err() == nil {
			// Race-instrumented executables delay normal exit by one second.
			waitLimit = 5 * time.Second
		}
		select {
		case err := <-waited:
			if result == nil && ctx.Err() == nil && err != nil {
				result = fmt.Errorf("filesystem listing helper: %w", err)
			}
		case <-time.After(waitLimit):
			_ = command.Process.Kill()
			// A kernel-uninterruptible process can be reaped only once its syscall
			// returns. Keep the caller bounded; this single waiter owns that reap.
			result = errors.Join(result, errors.New("filesystem listing helper did not exit after cancellation"))
		}
	}()
	if err := gob.NewEncoder(input).Encode(request); err != nil {
		return err
	}
	decoder := gob.NewDecoder(output)
	for {
		var record fsListRecord
		if err := decoder.Decode(&record); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("decode filesystem listing: %w", err)
		}
		if record.Done {
			complete = true
			return nil
		}
		if record.Error != nil {
			record.Content.Err = probe.NewError(record.Error.decode())
		}
		if !sendFSListContent(ctx, out, &record.Content) {
			return ctx.Err()
		}
	}
}
