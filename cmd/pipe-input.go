package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const pipeInputArg = "--oc-internal-pipe-input"

type pipeInputReader struct {
	io.ReadCloser
	wait func() error
}

func (r pipeInputReader) Close() error {
	err := r.ReadCloser.Close()
	// Wait closes StdoutPipe after a successful EOF; the filesystem writer
	// still closes its input before committing the staging file.
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

func (r pipeInputReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err == io.EOF {
		if helperErr := r.wait(); helperErr != nil {
			return n, helperErr
		}
	}
	return n, err
}

// Inherited stdin may use an uninterruptible synchronous read. Isolate that
// read in a child, and let the parent read a pollable pipe that cancellation
// can close. The helper runs before configuration or signal setup.
func init() {
	if len(os.Args) == 2 && os.Args[1] == pipeInputArg {
		// Stderr is a private liveness pipe, drained only by the parent. It
		// breaks on parent death even if the upstream stdin stays open.
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				if _, err := os.Stderr.Write([]byte{0}); err != nil {
					os.Exit(1)
				}
				<-ticker.C
			}
		}()
		if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func openPipeInput(ctx context.Context) (io.ReadCloser, func(), error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	command := exec.CommandContext(ctx, executable, pipeInputArg)
	command.Stdin = os.Stdin
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	control, err := command.StderrPipe()
	if err != nil {
		output.Close()
		return nil, nil, err
	}
	if err = command.Start(); err != nil {
		output.Close()
		control.Close()
		return nil, nil, err
	}
	controlDone := make(chan struct{})
	go func() { defer close(controlDone); _, _ = io.Copy(io.Discard, control) }()
	canceled := make(chan struct{})
	var waited sync.Once
	var waitErr error
	wait := func() error { waited.Do(func() { waitErr = command.Wait() }); return waitErr }
	stop := context.AfterFunc(ctx, func() { defer close(canceled); output.Close(); command.Process.Kill() })
	cleanup := func() {
		if !stop() {
			<-canceled
		}
		output.Close()
		command.Process.Kill()
		wait()
		control.Close()
		<-controlDone
	}
	return pipeInputReader{ReadCloser: output, wait: wait}, cleanup, nil
}
