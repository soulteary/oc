package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

const fsWatchStatArg = "--oc-internal-watch-stat"

// Stat can block in a filesystem syscall. Isolate it in one reusable child per
// watcher so cancellation can close the pipes and terminate the child instead
// of abandoning a goroutine blocked in the kernel. The helper never loads user
// configuration, performs network requests, or modifies files.
func init() {
	if len(os.Args) != 2 || os.Args[1] != fsWatchStatArg {
		return
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	requests := make(chan string, 1)
	// Read independently of Stat: losing the parent closes stdin, so even an
	// in-flight filesystem lookup cannot leave an orphan helper on parent exit.
	go func() {
		for {
			var path string
			if err := decoder.Decode(&path); err != nil {
				os.Exit(0)
			}
			requests <- path
		}
	}()
	for path := range requests {
		result := fsWatchFileState{}
		info, err := os.Stat(path)
		if err != nil {
			result.Error = err.Error()
			result.NotExist = os.IsNotExist(err)
		} else {
			result.Size = info.Size()
			result.ModTime = info.ModTime()
			result.Directory = info.IsDir()
		}
		if err := encoder.Encode(result); err != nil {
			os.Exit(1)
		}
	}
}

type fsWatchFileState struct {
	Size      int64
	ModTime   time.Time
	Directory bool
	Error     string
	NotExist  bool
}

type fsWatchStatWorker struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	encoder *json.Encoder
	decoder *json.Decoder
}

func (w *fsWatchStatWorker) close() {
	if w.command == nil {
		return
	}
	_ = w.input.Close()
	_ = w.output.Close()
	_ = w.command.Process.Kill()
	_ = w.command.Wait()
	w.command = nil
}

func (w *fsWatchStatWorker) stat(ctx context.Context, path string) (fsWatchFileState, error) {
	if err := ctx.Err(); err != nil {
		return fsWatchFileState{}, err
	}
	if w.command == nil {
		executable, err := os.Executable()
		if err != nil {
			return fsWatchFileState{}, err
		}
		command := exec.CommandContext(ctx, executable, fsWatchStatArg)
		input, err := command.StdinPipe()
		if err != nil {
			return fsWatchFileState{}, err
		}
		output, err := command.StdoutPipe()
		if err != nil {
			_ = input.Close()
			return fsWatchFileState{}, err
		}
		if err := command.Start(); err != nil {
			_ = input.Close()
			_ = output.Close()
			return fsWatchFileState{}, fmt.Errorf("start file watch metadata helper: %w", err)
		}
		w.command, w.input, w.output = command, input, output
		w.encoder, w.decoder = json.NewEncoder(input), json.NewDecoder(output)
	}
	type response struct {
		info fsWatchFileState
		err  error
	}
	result := make(chan response, 1)
	// At most one request is outstanding; cancellation closes both pipes and
	// joins this goroutine before returning.
	go func() {
		r := response{}
		r.err = w.encoder.Encode(path)
		if r.err == nil {
			r.err = w.decoder.Decode(&r.info)
		}
		result <- r
	}()
	var r response
	select {
	case <-ctx.Done():
		w.close()
		<-result
		return fsWatchFileState{}, ctx.Err()
	case r = <-result:
	}
	if r.err != nil {
		w.close()
		return fsWatchFileState{}, fmt.Errorf("file watch metadata helper: %w", r.err)
	}
	if r.info.Error != "" {
		if r.info.NotExist {
			return r.info, os.ErrNotExist
		}
		return r.info, errors.New(r.info.Error)
	}
	return r.info, nil
}
