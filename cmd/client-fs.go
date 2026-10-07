/*
 * MinIO Client (C) 2015-2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this fs except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/xattr"
	"github.com/soulteary/mc/internal/notify"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/encrypt"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"github.com/minio/minio-go/v7/pkg/notification"
	"github.com/minio/minio-go/v7/pkg/replication"
	"github.com/soulteary/mc/pkg/disk"
	"github.com/soulteary/mc/pkg/hookreader"
	"github.com/soulteary/mc/pkg/ioutils"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
)

// filesystem client
type fsClient struct {
	PathURL *ClientURL
}

const (
	partSuffix       = ".part.minio"
	partialDirPrefix = ".oc-part-"
	slashSeperator   = "/"
	metadataKey      = "X-Amz-Meta-Mc-Attrs"
	metadataKeyS3Cmd = "X-Amz-Meta-S3cmd-Attrs"
)

var ( // GOOS specific ignore list.
	ignoreFiles = map[string][]string{
		"darwin":  {"*.DS_Store"},
		"default": {"lost+found"},
	}
)

// fsNew - instantiate a new fs
func fsNew(path string) (Client, *probe.Error) {
	if strings.TrimSpace(path) == "" {
		return nil, probe.NewError(EmptyPath{})
	}
	return &fsClient{
		PathURL: newClientURL(normalizePath(path)),
	}, nil
}

func isNotSupported(e error) bool {
	if e == nil {
		return false
	}
	errno := e.(*xattr.Error)
	if errno == nil {
		return false
	}

	// check if filesystem supports extended attributes
	return errno.Err == syscall.Errno(syscall.ENOTSUP) || errno.Err == syscall.Errno(syscall.EOPNOTSUPP)
}

// isIgnoredFile returns true if 'filename' is on the exclude list.
func isIgnoredFile(filename string) bool {
	matchFile := filepath.Base(filename)
	if strings.HasSuffix(matchFile, partSuffix) || isPartialDir(filepath.Dir(filename)) {
		return true
	}

	// OS specific ignore list.
	for _, ignoredFile := range ignoreFiles[runtime.GOOS] {
		matched, e := filepath.Match(ignoredFile, matchFile)
		if e != nil {
			panic(e)
		}
		if matched {
			return true
		}
	}

	// Default ignore list for all OSes.
	for _, ignoredFile := range ignoreFiles["default"] {
		matched, e := filepath.Match(ignoredFile, matchFile)
		if e != nil {
			panic(e)
		}
		if matched {
			return true
		}
	}

	return false
}

// URL get url.
func (f *fsClient) GetURL() ClientURL {
	return *f.PathURL
}

// Select replies a stream of query results.
func (f *fsClient) Select(ctx context.Context, expression string, sse encrypt.ServerSide, opts SelectObjectOpts) (io.ReadCloser, *probe.Error) {
	return nil, probe.NewError(APINotImplemented{
		API:     "Select",
		APIType: "filesystem",
	})
}

// Watches for all fs events on an input path.
func (f *fsClient) Watch(ctx context.Context, options WatchOptions) (*WatchObject, *probe.Error) {
	if err := ctx.Err(); err != nil {
		return nil, probe.NewError(err)
	}
	eventChan := make(chan []EventInfo)
	errorChan := make(chan *probe.Error, 1)
	doneChan := make(chan struct{})
	// Bound queued notifications. Like the OS watcher, notify may drop events
	// when a consumer cannot keep up; this is a live stream, not a durable log.
	in, out := PipeChan(1000)

	var fsEvents []notify.Event
	for _, event := range options.Events {
		switch event {
		case "put":
			fsEvents = append(fsEvents, EventTypePut...)
		case "delete":
			fsEvents = append(fsEvents, EventTypeDelete...)
		case "get":
			fsEvents = append(fsEvents, EventTypeGet...)
		default:
			// Event type not supported by FS client, such as
			// bucket creation or deletion, ignore it.
		}
	}

	fsEvents = append(fsEvents, extraFSWatchEvents()...)
	// Set up a watchpoint listening for events within a directory tree rooted
	// at current working directory. Dispatch remove events to c.
	recursivePath := f.PathURL.Path
	if options.Recursive {
		recursivePath = f.PathURL.Path + "..."
	}
	if e := notify.Watch(recursivePath, in, fsEvents...); e != nil {
		notify.Stop(in)
		return nil, probe.NewError(e)
	}

	wo := &WatchObject{EventInfoChan: eventChan, ErrorChan: errorChan, DoneChan: doneChan}
	go forwardFSWatchEvents(ctx, wo, out, func() { notify.Stop(in) })
	return wo, nil
}

// Only the forwarding goroutine closes its output channels, after stopping the
// native watcher. Both context cancellation and the legacy DoneChan stop it.
func forwardFSWatchEvents(ctx context.Context, wo *WatchObject, events <-chan notify.EventInfo, stop func()) {
	worker := &fsWatchStatWorker{}
	forwardFSWatchEventsWithStat(ctx, wo, events, func() { worker.close(); stop() }, worker.stat)
}

func forwardFSWatchEventsWithStat(ctx context.Context, wo *WatchObject, events <-chan notify.EventInfo, stop func(), stat func(context.Context, string) (fsWatchFileState, error)) {
	ctx, cancel := context.WithCancelCause(ctx)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-wo.DoneChan:
				cancel(context.Canceled)
				return
			case <-ticker.C:
				if cap(events) > 0 && len(events) == cap(events) {
					cancel(fsWatchOverflow{})
					return
				}
			}
		}
	}()
	defer func() {
		cause := context.Cause(ctx)
		cancel(context.Canceled)
		<-finished
		stop()
		if _, ok := cause.(fsWatchOverflow); ok {
			// Production error channels have one reserved slot. Never let a
			// stalled consumer prevent cleanup.
			select {
			case <-wo.ErrorChan:
			default:
			}
			select {
			case wo.ErrorChan <- probe.NewError(cause):
			default:
			}
		}
		close(wo.EventInfoChan)
		close(wo.ErrorChan)
	}()
	const timeFormatFS = "2006-01-02T15:04:05.000Z"
	for {
		if cap(events) > 0 && len(events) == cap(events) {
			cancel(fsWatchOverflow{})
			return
		}
		var event notify.EventInfo
		select {
		case <-ctx.Done():
			return
		case <-wo.DoneChan:
			return
		case value, ok := <-events:
			if !ok {
				return
			}
			event = value
			if cap(events) > 0 && len(events) >= cap(events)-1 {
				cancel(fsWatchOverflow{})
				return
			}
		}
		if fsWatchNeedsRescan(event) {
			cancel(fsWatchOverflow{native: true})
			return
		}
		if isIgnoredFile(event.Path()) {
			continue
		}
		info := EventInfo{Time: UTCNow().Format(timeFormatFS), Path: event.Path()}
		switch {
		case IsPutEvent(event.Event()):
			state, err := stat(ctx, event.Path())
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if os.IsNotExist(err) {
					continue
				}
				select {
				case wo.ErrorChan <- probe.NewError(err):
				case <-ctx.Done():
					return
				case <-wo.DoneChan:
					return
				}
				continue
			}
			if state.Directory {
				continue
			}
			info.Size, info.Type = state.Size, notification.ObjectCreatedPut
		case IsDeleteEvent(event.Event()):
			info.Type = notification.ObjectRemovedDelete
		case IsGetEvent(event.Event()):
			info.Type = notification.ObjectAccessedGet
		default:
			continue
		}
		select {
		case wo.EventInfoChan <- []EventInfo{info}:
		case <-ctx.Done():
			return
		case <-wo.DoneChan:
			return
		}
	}
}

func preserveAttributes(fd *os.File, attr map[string]string) *probe.Error {
	if val, ok := attr["mode"]; ok {
		mode, e := strconv.ParseUint(val, 0, 32)
		if e == nil {
			// Attempt to change the file mode.
			if e = fd.Chmod(os.FileMode(mode)); e != nil {
				return probe.NewError(e)
			}
		}
	}

	var uid, gid int
	var e error
	if val, ok := attr["uid"]; ok {
		uid, e = strconv.Atoi(val)
		if e != nil {
			uid = -1
		}
	}

	if val, ok := attr["gid"]; ok {
		gid, e = strconv.Atoi(val)
		if e != nil {
			gid = -1
		}
	}

	// Attempt to change the owner.
	if e = fd.Chown(uid, gid); e != nil {
		return probe.NewError(e)
	}

	return nil
}

/// Object operations.

func (f *fsClient) put(ctx context.Context, reader io.Reader, size int64, progress io.Reader, opts PutOptions) (written int64, putErr *probe.Error) {
	if err := ctx.Err(); err != nil {
		return 0, probe.NewError(err)
	}
	// ContentType is not handled on purpose.
	// For filesystem this is a redundant information.

	// Extract dir name.
	objectDir, objectName := filepath.Split(f.PathURL.Path)

	if objectDir != "" {
		// Create any missing top level directories.
		if e := os.MkdirAll(objectDir, 0777); e != nil {
			err := f.toClientError(e, f.PathURL.Path)
			return 0, err.Trace(f.PathURL.Path)
		}

		// Check if object name is empty, it must be an empty directory
		if objectName == "" {
			return 0, nil
		}
	}

	objectPath := f.PathURL.Path

	// Store data and a target manifest in a private, unique staging directory.
	// This preserves long filenames and lets incomplete listings recover the target.
	tmpFile, stage, e := createLocalPartial(objectPath)
	if e != nil {
		return 0, f.toClientError(e, objectPath)
	}
	defer func() {
		if err := stage.cleanup(); err != nil {
			var original error
			if putErr != nil {
				original = putErr.ToGoError()
			}
			putErr = probe.NewError(errors.Join(original, fmt.Errorf("clean up local staging: %w", err)))
		}
	}()
	defer tmpFile.Close()
	objectPartPath := filepath.Join(stage.dir, partialDataName)

	attr := make(map[string]string)
	if _, ok := opts.metadata[metadataKey]; ok && opts.isPreserve {
		attr, e = parseAttribute(opts.metadata)
		if e != nil {
			tmpFile.Close()
			return 0, probe.NewError(e)
		}
		err := preserveAttributes(tmpFile, attr)
		if err != nil {
			console.Println(console.Colorize("Error", fmt.Sprintf("unable to preserve attributes, continuing to copy the content %s\n", err.ToGoError())))
		}
	}

	totalWritten, e := io.Copy(tmpFile, hookreader.NewHook(fsContextReader{ctx: ctx, reader: reader}, progress))
	if e != nil {
		tmpFile.Close()
		return totalWritten, probe.NewError(e)
	}

	// Close the input reader as well, if possible.
	closer, ok := reader.(io.Closer)
	if ok {
		if e = closer.Close(); e != nil {
			tmpFile.Close()
			return totalWritten, probe.NewError(e)
		}
	}

	// Close the file before renaming, we need to do this
	// specifically for windows users - windows explicitly
	// disallows renames on Open() fd's by default.
	if e = tmpFile.Close(); e != nil {
		return totalWritten, probe.NewError(e)
	}

	// Nonnegative sizes are known, including an explicitly empty object.
	if size >= 0 {
		// Unexpected EOF reached (less data was written than expected).
		if totalWritten < size {
			return totalWritten, probe.NewError(UnexpectedEOF{
				TotalSize:    size,
				TotalWritten: totalWritten,
			})
		}
		// Unexpected ExcessRead (more data was written than expected).
		if totalWritten > size {
			return totalWritten, probe.NewError(UnexpectedExcessRead{
				TotalSize:    size,
				TotalWritten: totalWritten,
			})
		}
	}

	if len(attr) != 0 && opts.isPreserve {
		atime, mtime, err := parseAtimeMtime(attr)
		if err != nil {
			return totalWritten, err.Trace()
		}
		if !atime.IsZero() && !mtime.IsZero() {
			if e := stage.root.Chtimes(partialDataName, atime, mtime); e != nil {
				return totalWritten, probe.NewError(e)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return totalWritten, probe.NewError(err)
	}
	// Commit only after the data and metadata have both been validated.
	if e = stage.commit(objectPath); e != nil {
		err := f.toClientError(e, objectPath)
		return totalWritten, err.Trace(objectPartPath, objectPath)
	}

	return totalWritten, nil
}

// Put - create a new file with metadata.
func (f *fsClient) Put(ctx context.Context, reader io.Reader, size int64, progress io.Reader, opts PutOptions) (int64, *probe.Error) {
	return f.put(ctx, reader, size, progress, opts)
}

// ShareDownload - share download not implemented for filesystem.
func (f *fsClient) ShareDownload(ctx context.Context, versionID string, expires time.Duration) (string, *probe.Error) {
	return "", probe.NewError(APINotImplemented{
		API:     "ShareDownload",
		APIType: "filesystem",
	})
}

// ShareUpload - share upload not implemented for filesystem.
func (f *fsClient) ShareUpload(ctx context.Context, startsWith bool, expires time.Duration, contentType string) (string, map[string]string, *probe.Error) {
	return "", nil, probe.NewError(APINotImplemented{
		API:     "ShareUpload",
		APIType: "filesystem",
	})
}

// Copy - copy data from source to destination
func (f *fsClient) Copy(ctx context.Context, source string, opts CopyOptions, progress io.Reader) *probe.Error {
	rc, e := os.Open(source)
	if e != nil {
		err := f.toClientError(e, source)
		return err.Trace(source)
	}
	defer rc.Close()

	putOpts := PutOptions{
		metadata:   opts.metadata,
		isPreserve: opts.isPreserve,
	}

	destination := f.PathURL.Path
	if _, err := f.put(ctx, rc, opts.size, progress, putOpts); err != nil {
		return err.Trace(destination, source)
	}
	return nil
}

// Get returns reader and any additional metadata.
func (f *fsClient) Get(ctx context.Context, opts GetOptions) (io.ReadCloser, *probe.Error) {
	fileData, e := os.Open(f.PathURL.Path)
	if e != nil {
		err := f.toClientError(e, f.PathURL.Path)
		return nil, err.Trace(f.PathURL.Path)
	}
	return fileData, nil
}

// Check if the given error corresponds to ENOTEMPTY for unix
// and ERROR_DIR_NOT_EMPTY for windows (directory not empty).
func isSysErrNotEmpty(err error) bool {
	if err == syscall.ENOTEMPTY {
		return true
	}
	if pathErr, ok := err.(*os.PathError); ok {
		if runtime.GOOS == "windows" {
			if errno, _ok := pathErr.Err.(syscall.Errno); _ok && errno == 0x91 {
				// ERROR_DIR_NOT_EMPTY
				return true
			}
		}
		if pathErr.Err == syscall.ENOTEMPTY {
			return true
		}
	}
	return false
}

// deleteFile deletes a file path if its empty. If it's successfully deleted,
// it will recursively delete empty parent directories
// until it finds one with files in it. Returns nil for a non-empty directory.
func deleteFile(deletePath string) error {
	// Attempt to remove path.
	if e := os.Remove(deletePath); e != nil {
		if isSysErrNotEmpty(e) {
			return nil
		}
		return e
	}

	// Trailing slash is removed when found to ensure
	// slashpath.Dir() to work as intended.
	parentPath := strings.TrimSuffix(deletePath, slashSeperator)
	parentPath = path.Dir(parentPath)

	if parentPath != "." {
		return deleteFile(parentPath)
	}

	return nil
}

// Remove - remove entry read from clientContent channel.
func (f *fsClient) Remove(ctx context.Context, isIncomplete, isRemoveBucket, isBypass bool, contentCh <-chan *ClientContent) <-chan *probe.Error {
	errorCh := make(chan *probe.Error)

	// Goroutine reads from contentCh and removes the entry in content.
	go func() {
		defer close(errorCh)

		sendError := func(err *probe.Error) bool {
			select {
			case errorCh <- err:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			var content *ClientContent
			select {
			case <-ctx.Done():
				return
			case next, ok := <-contentCh:
				if !ok {
					return
				}
				content = next
			}
			if ctx.Err() != nil {
				return
			}
			if content.Err != nil {
				if !sendError(content.Err) {
					return
				}
				continue
			}
			name := content.URL.Path
			// Remove verified staged data or legacy partial files.
			if isIncomplete {
				partials := []*ClientContent{content}
				if content.fsPartialPath == "" {
					var e error
					partials, e = localPartials(name)
					if e != nil {
						if !sendError(f.toClientError(e, name)) {
							return
						}
						continue
					}
				}
				if len(partials) == 0 {
					if !sendError(f.toClientError(os.ErrNotExist, name)) {
						return
					}
					continue
				}
				for _, partial := range partials {
					if ctx.Err() != nil {
						return
					}
					if e := removeLocalPartial(partial); e != nil && !os.IsNotExist(e) {
						if !sendError(f.toClientError(e, partial.fsPartialPath)) {
							return
						}
					}
				}
				continue
			}
			e := deleteFile(name)
			if e == nil {
				continue
			}
			if os.IsNotExist(e) && isRemoveBucket {
				// ignore PathNotFound for dir removal.
				return
			}
			if os.IsPermission(e) {
				// Ignore permission error.
				if !sendError(probe.NewError(PathInsufficientPermission{Path: content.URL.Path})) {
					return
				}
			} else {
				if !sendError(probe.NewError(e)) {
					return
				}
				return
			}
		}
	}()

	return errorCh
}

// List - list files and folders.
func (f *fsClient) List(ctx context.Context, opts ListOptions) <-chan *ClientContent {
	if opts.Incomplete {
		return f.listLocalPartials(ctx, opts.Recursive)
	}
	return f.listIsolated(ctx, opts)
}

// Run only inside the listing helper: filesystem calls may block independently
// of context, so the caller owns the helper's process and communication pipes.
func (f *fsClient) listInProcess(ctx context.Context, opts ListOptions) <-chan *ClientContent {
	contentCh := make(chan *ClientContent)
	filteredCh := make(chan *ClientContent)
	ctx, cancel := context.WithCancel(ctx)
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		if opts.Recursive {
			if opts.ShowDir == DirNone {
				f.listRecursiveInRoutine(ctx, contentCh, opts.WithMetadata)
			} else {
				f.listDirOpt(ctx, contentCh, opts.Incomplete, opts.WithMetadata, opts.ShowDir)
			}
		} else {
			f.listInRoutine(ctx, contentCh, opts.WithMetadata)
		}
	}()
	// Join the producer before closing the public channel, including cancellation.
	go func() {
		defer close(filteredCh)
		defer func() { cancel(); <-producerDone }()
		for {
			var c *ClientContent
			select {
			case <-ctx.Done():
				return
			case value, ok := <-contentCh:
				if !ok {
					return
				}
				c = value
			}
			if c.Err == nil && (isPartialDir(c.URL.Path) || isPartialDir(filepath.Dir(c.URL.Path)) || strings.HasSuffix(c.URL.Path, partSuffix)) {
				continue
			}
			if !sendFSListContent(ctx, filteredCh, c) {
				return
			}
		}
	}()
	return filteredCh
}

func sendFSListContent(ctx context.Context, ch chan<- *ClientContent, content *ClientContent) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case ch <- content:
		return true
	case <-ctx.Done():
		return false
	}
}

// byDirName implements sort.Interface.
type byDirName []os.FileInfo

func (f byDirName) Len() int { return len(f) }
func (f byDirName) Less(i, j int) bool {
	// For directory add an ending separator fortrue lexical
	// order.
	if f[i].Mode().IsDir() {
		return f[i].Name()+string(filepath.Separator) < f[j].Name()
	}
	// For directory add an ending separator for true lexical
	// order.
	if f[j].Mode().IsDir() {
		return f[i].Name() < f[j].Name()+string(filepath.Separator)
	}
	return f[i].Name() < f[j].Name()
}
func (f byDirName) Swap(i, j int) { f[i], f[j] = f[j], f[i] }

// readDir reads the directory named by dirname and returns
// a list of sorted directory entries.
func readDir(dirname string) ([]os.FileInfo, error) {
	f, e := os.Open(dirname)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	list, e := f.Readdir(-1)
	if e != nil {
		return nil, e
	}
	sort.Sort(byDirName(list))
	return list, nil
}

// listPrefixes - list all files for any given prefix.
func (f *fsClient) listPrefixes(ctx context.Context, prefix string, contentCh chan<- *ClientContent) {
	if ctx.Err() != nil {
		return
	}
	dirName := filepath.Dir(prefix)
	files, e := readDir(dirName)
	if e != nil {
		err := f.toClientError(e, dirName)
		if !sendFSListContent(ctx, contentCh, &ClientContent{
			Err: err.Trace(dirName),
		}) {
			return
		}
		return
	}
	for _, fi := range files {
		if ctx.Err() != nil {
			return
		}
		// Skip ignored files.
		if isIgnoredFile(fi.Name()) {
			continue
		}

		file := filepath.Join(dirName, fi.Name())
		if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
			st, e := os.Stat(file)
			if e != nil {
				// Ignore any errors on symlink
				continue
			}
			if strings.HasPrefix(file, prefix) {
				if !sendFSListContent(ctx, contentCh, &ClientContent{
					URL:  *newClientURL(file),
					Time: st.ModTime(),
					Size: st.Size(),
					Type: st.Mode(),
					Err:  nil,
				}) {
					return
				}
				continue
			}
		}
		if strings.HasPrefix(file, prefix) {
			if !sendFSListContent(ctx, contentCh, &ClientContent{
				URL:  *newClientURL(file),
				Time: fi.ModTime(),
				Size: fi.Size(),
				Type: fi.Mode(),
				Err:  nil,
			}) {
				return
			}
		}
	}
}

func (f *fsClient) listInRoutine(ctx context.Context, contentCh chan<- *ClientContent, isMetadata bool) {
	// close the channel when the function returns.
	defer close(contentCh)
	if ctx.Err() != nil {
		return
	}

	// save pathURL and file path for further usage.
	pathURL := *f.PathURL
	fpath := pathURL.Path

	fst, err := f.fsStat(false)
	if err != nil {
		if _, ok := err.ToGoError().(PathNotFound); ok {
			// If file does not exist treat it like a prefix and list all prefixes if any.
			prefix := fpath
			f.listPrefixes(ctx, prefix, contentCh)
			return
		}
		// For all other errors we return genuine error back to the caller.
		if !sendFSListContent(ctx, contentCh, &ClientContent{Err: err.Trace(fpath)}) {
			return
		}
		return
	}

	// Now if the file exists and doesn't end with a separator ('/') do not traverse it.
	// If the directory doesn't end with a separator, do not traverse it.
	if !strings.HasSuffix(fpath, string(pathURL.Separator)) && fst.Mode().IsDir() && fpath != "." {
		f.listPrefixes(ctx, fpath, contentCh)
		return
	}

	// If we really see the directory.
	switch fst.Mode().IsDir() {
	case true:
		files, e := readDir(fpath)
		if e != nil {
			if !sendFSListContent(ctx, contentCh, &ClientContent{Err: probe.NewError(e)}) {
				return
			}
			return
		}
		for _, file := range files {
			if ctx.Err() != nil {
				return
			}
			fi := file
			if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
				fp := filepath.Join(fpath, fi.Name())
				fi, e = os.Stat(fp)
				if e != nil {
					// Ignore all errors on symlinks
					continue
				}
			}
			if fi.Mode().IsRegular() || fi.Mode().IsDir() {
				pathURL = *f.PathURL
				pathURL.Path = filepath.Join(pathURL.Path, fi.Name())

				// Skip ignored files.
				if isIgnoredFile(fi.Name()) {
					continue
				}

				if !sendFSListContent(ctx, contentCh, &ClientContent{
					URL:  pathURL,
					Time: fi.ModTime(),
					Size: fi.Size(),
					Type: fi.Mode(),
					Err:  nil,
				}) {
					return
				}
			}
		}
	default:
		if !sendFSListContent(ctx, contentCh, &ClientContent{
			URL:  pathURL,
			Time: fst.ModTime(),
			Size: fst.Size(),
			Type: fst.Mode(),
			Err:  nil,
		}) {
			return
		}
	}
}

// List files recursively using non-recursive mode.
func (f *fsClient) listDirOpt(ctx context.Context, contentCh chan *ClientContent, isIncomplete bool, isMetadata bool, dirOpt DirOpt) {
	defer close(contentCh)
	if ctx.Err() != nil {
		return
	}

	// Trim trailing / or \.
	currentPath := f.PathURL.Path
	currentPath = strings.TrimSuffix(currentPath, "/")
	if runtime.GOOS == "windows" {
		currentPath = strings.TrimSuffix(currentPath, `\`)
	}

	// Closure function reads currentPath and sends to contentCh. If a directory is found, it lists the directory content recursively.
	var listDir func(currentPath string) bool
	listDir = func(currentPath string) (isStop bool) {
		if ctx.Err() != nil {
			return true
		}
		files, e := readDir(currentPath)
		if e != nil {
			if os.IsPermission(e) {
				return !sendFSListContent(ctx, contentCh, &ClientContent{
					Err: probe.NewError(PathInsufficientPermission{
						Path: currentPath,
					}),
				})
			}

			if !sendFSListContent(ctx, contentCh, &ClientContent{Err: probe.NewError(e)}) {
				return true
			}
			return true
		}

		for _, file := range files {
			if ctx.Err() != nil {
				return true
			}
			name := filepath.Join(currentPath, file.Name())
			content := ClientContent{
				URL:  *newClientURL(name),
				Time: file.ModTime(),
				Size: file.Size(),
				Type: file.Mode(),
				Err:  nil,
			}
			if file.Mode().IsDir() {
				if dirOpt == DirFirst && !isIncomplete {
					if !sendFSListContent(ctx, contentCh, &content) {
						return true
					}
				}
				if listDir(filepath.Join(name)) {
					return true
				}
				if dirOpt == DirLast && !isIncomplete {
					if !sendFSListContent(ctx, contentCh, &content) {
						return true
					}
				}

				continue
			}

			if !sendFSListContent(ctx, contentCh, &content) {
				return true
			}
		}

		return false
	}

	// listDir() does not send currentPath to contentCh.  We send it here depending on dirOpt.

	if dirOpt == DirFirst && !isIncomplete {
		if !sendFSListContent(ctx, contentCh, &ClientContent{URL: *newClientURL(currentPath), Type: os.ModeDir}) {
			return
		}
	}

	listDir(currentPath)

	if dirOpt == DirLast && !isIncomplete {
		if !sendFSListContent(ctx, contentCh, &ClientContent{URL: *newClientURL(currentPath), Type: os.ModeDir}) {
			return
		}
	}
}

func (f *fsClient) listRecursiveInRoutine(ctx context.Context, contentCh chan *ClientContent, isMetadata bool) {
	// close channels upon return.
	defer close(contentCh)
	if ctx.Err() != nil {
		return
	}
	var dirName string
	var filePrefix string
	pathURL := *f.PathURL
	visitFS := func(fp string, fi os.FileInfo, e error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// If file path ends with filepath.Separator and equals to root path, skip it.
		if strings.HasSuffix(fp, string(pathURL.Separator)) {
			if fp == dirName {
				return nil
			}
		}
		// We would never need to print system root path '/'.
		if fp == "/" {
			return nil
		}

		// Ignore files from ignore list.
		if e == nil && isIgnoredFile(fi.Name()) {
			return nil
		}

		/// In following situations we need to handle listing properly.
		// - When filepath is '/usr' and prefix is '/usr/bi'
		// - When filepath is '/usr/bin/subdir' and prefix is '/usr/bi'
		// - Do not check filePrefix if its '.'
		if filePrefix != "." {
			if !strings.HasPrefix(fp, filePrefix) &&
				!strings.HasPrefix(filePrefix, fp) {
				if e == nil {
					if fi.IsDir() {
						return ioutils.ErrSkipDir
					}
					return nil
				}
			}
			// - Skip when fp is /usr and prefix is '/usr/bi'
			// - Do not check filePrefix if its '.'
			if filePrefix != "." {
				if !strings.HasPrefix(fp, filePrefix) {
					return nil
				}
			}
		}
		if e != nil {
			// If operation is not permitted, we throw quickly back.
			if strings.Contains(e.Error(), "operation not permitted") {
				if !sendFSListContent(ctx, contentCh, &ClientContent{
					Err: probe.NewError(e),
				}) {
					return ctx.Err()
				}
				return nil
			}
			if os.IsPermission(e) {
				if !sendFSListContent(ctx, contentCh, &ClientContent{
					Err: probe.NewError(PathInsufficientPermission{Path: fp}),
				}) {
					return ctx.Err()
				}
				return nil
			}
			return e
		}
		if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
			fi, e = os.Stat(fp)
			if e != nil {
				// Ignore any errors for symlink
				return nil
			}
		}
		if fi.Mode().IsRegular() {
			if !sendFSListContent(ctx, contentCh, &ClientContent{
				URL:  *newClientURL(fp),
				Time: fi.ModTime(),
				Size: fi.Size(),
				Type: fi.Mode(),
				Err:  nil,
			}) {
				return ctx.Err()
			}
		}
		return nil
	}
	// No prefix to be filtered by default.
	filePrefix = ""
	// if f.Path ends with filepath.Separator - assuming it to be a directory and moving on.
	if strings.HasSuffix(pathURL.Path, string(pathURL.Separator)) {
		dirName = pathURL.Path
	} else {
		// if not a directory, take base path to navigate through WalkFunc.
		dirName = filepath.Dir(pathURL.Path)
		if !strings.HasSuffix(dirName, string(pathURL.Separator)) {
			// basepath truncates the filepath.Separator,
			// add it deligently useful for trimming file path inside WalkFunc
			dirName = dirName + string(pathURL.Separator)
		}
		// filePrefix is kept for filtering incoming contents through WalkFunc.
		filePrefix = pathURL.Path
	}
	// walks invokes our custom function.
	e := ioutils.FTW(dirName, visitFS)
	if e != nil {
		if !sendFSListContent(ctx, contentCh, &ClientContent{
			Err: probe.NewError(e),
		}) {
			return
		}
	}
}

// MakeBucket - create a new bucket.
func (f *fsClient) MakeBucket(ctx context.Context, region string, ignoreExisting, withLock bool) *probe.Error {
	// TODO: ignoreExisting has no effect currently. In the future, we want
	// to call os.Mkdir() when ignoredExisting is disabled and os.MkdirAll()
	// otherwise.
	// NOTE: withLock=true has no meaning here.
	e := os.MkdirAll(f.PathURL.Path, 0777)
	if e != nil {
		return probe.NewError(e)
	}
	return nil
}

// Set object lock configuration of bucket.
func (f *fsClient) SetObjectLockConfig(ctx context.Context, mode minio.RetentionMode, validity uint64, unit minio.ValidityUnit) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "SetObjectLockConfig",
		APIType: "filesystem",
	})
}

// Get object lock configuration of bucket.
func (f *fsClient) GetObjectLockConfig(ctx context.Context) (status string, mode minio.RetentionMode, validity uint64, unit minio.ValidityUnit, err *probe.Error) {
	return "", "", 0, "", probe.NewError(APINotImplemented{
		API:     "GetObjectLockConfig",
		APIType: "filesystem",
	})
}

// GetAccessRules - unsupported API
func (f *fsClient) GetAccessRules(ctx context.Context) (map[string]string, *probe.Error) {
	return map[string]string{}, probe.NewError(APINotImplemented{
		API:     "GetBucketPolicy",
		APIType: "filesystem",
	})
}

// Set object retention for a given object.
func (f *fsClient) PutObjectRetention(ctx context.Context, versionID string, mode minio.RetentionMode, retainUntilDate time.Time, bypassGovernance bool) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "PutObjectRetention",
		APIType: "filesystem",
	})
}

func (f *fsClient) GetObjectRetention(ctx context.Context, versionID string) (minio.RetentionMode, time.Time, *probe.Error) {
	return "", time.Time{}, probe.NewError(APINotImplemented{
		API:     "GetObjectRetention",
		APIType: "filesystem",
	})
}

// Set object legal hold for a given object.
func (f *fsClient) PutObjectLegalHold(ctx context.Context, versionID string, lhold minio.LegalHoldStatus) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "PutObjectLegalHold",
		APIType: "filesystem",
	})
}

// Get object legal hold for a given object.
func (f *fsClient) GetObjectLegalHold(ctx context.Context, versionID string) (minio.LegalHoldStatus, *probe.Error) {
	return "", probe.NewError(APINotImplemented{
		API:     "GetObjectLegalHold",
		APIType: "filesystem",
	})
}

// GetAccess - get access policy permissions.
func (f *fsClient) GetAccess(ctx context.Context) (access string, policyJSON string, err *probe.Error) {
	// For windows this feature is not implemented.
	if runtime.GOOS == "windows" {
		return "", "", probe.NewError(APINotImplemented{API: "GetAccess", APIType: "filesystem"})
	}
	st, err := f.fsStat(false)
	if err != nil {
		return "", "", err.Trace(f.PathURL.String())
	}
	if !st.Mode().IsDir() {
		return "", "", probe.NewError(APINotImplemented{API: "GetAccess", APIType: "filesystem"})
	}
	// Mask with os.ModePerm to get only inode permissions
	switch st.Mode() & os.ModePerm {
	case os.FileMode(0777):
		return "readwrite", "", nil
	case os.FileMode(0555):
		return "readonly", "", nil
	case os.FileMode(0333):
		return "writeonly", "", nil
	}
	return "none", "", nil
}

// SetAccess - set access policy permissions.
func (f *fsClient) SetAccess(ctx context.Context, access string, isJSON bool) *probe.Error {
	// For windows this feature is not implemented.
	// JSON policy for fs is not yet implemented.
	if runtime.GOOS == "windows" || isJSON {
		return probe.NewError(APINotImplemented{API: "SetAccess", APIType: "filesystem"})
	}
	st, err := f.fsStat(false)
	if err != nil {
		return err.Trace(f.PathURL.String())
	}
	if !st.Mode().IsDir() {
		return probe.NewError(APINotImplemented{API: "SetAccess", APIType: "filesystem"})
	}
	var mode os.FileMode
	switch access {
	case "readonly":
		mode = os.FileMode(0555)
	case "writeonly":
		mode = os.FileMode(0333)
	case "readwrite":
		mode = os.FileMode(0777)
	case "none":
		mode = os.FileMode(0755)
	}
	e := os.Chmod(f.PathURL.Path, mode)
	if e != nil {
		return probe.NewError(e)
	}
	return nil
}

// Stat - get metadata from path.
func (f *fsClient) Stat(ctx context.Context, opts StatOptions) (content *ClientContent, err *probe.Error) {
	st, err := f.fsStat(opts.incomplete)
	if err != nil {
		return nil, err.Trace(f.PathURL.String())
	}

	content = &ClientContent{}
	content.URL = *f.PathURL
	content.Size = st.Size()
	content.Time = st.ModTime()
	content.Type = st.Mode()
	content.Metadata = map[string]string{
		"Content-Type": guessURLContentType(f.PathURL.Path),
	}

	path := f.PathURL.String()
	// Populates meta data with file system attribute only in case of
	// when preserve flag is passed.
	if opts.preserve {
		fileAttr, err := disk.GetFileSystemAttrs(path)
		if err != nil {
			return content, nil
		}
		metaData, pErr := getAllXattrs(path)
		if pErr != nil {
			return content, nil
		}
		for k, v := range metaData {
			content.Metadata[k] = v
		}
		content.Metadata[metadataKey] = fileAttr
	}

	return content, nil
}

// toClientError error constructs a typed client error for known filesystem errors.
func (f *fsClient) toClientError(e error, fpath string) *probe.Error {
	if os.IsPermission(e) {
		return probe.NewError(PathInsufficientPermission{Path: fpath})
	}
	if os.IsNotExist(e) {
		return probe.NewError(PathNotFound{Path: fpath})
	}
	if errors.Is(e, syscall.ELOOP) {
		return probe.NewError(TooManyLevelsSymlink{Path: fpath})
	}
	return probe.NewError(e)
}

// fsStat - wrapper function to get file stat.
func (f *fsClient) fsStat(isIncomplete bool) (os.FileInfo, *probe.Error) {
	fpath := f.PathURL.Path

	// Check if the path corresponds to a directory and returns
	// the successful result whether isIncomplete is specified or not.
	st, e := os.Stat(fpath)
	if e == nil && st.IsDir() {
		return st, nil
	}

	if isIncomplete {
		partials, err := localPartials(fpath)
		if err != nil {
			return nil, f.toClientError(err, fpath)
		}
		if len(partials) == 0 {
			return nil, f.toClientError(os.ErrNotExist, fpath)
		}
		if partials[0].fsPartialInfo == nil {
			marker, err := os.Stat(filepath.Join(filepath.Dir(partials[0].fsPartialPath), partialManifestName))
			if err != nil {
				return nil, f.toClientError(err, fpath)
			}
			return emptyPartialInfo{FileInfo: marker, target: filepath.Base(fpath)}, nil
		}
		return partials[0].fsPartialInfo, nil
	}

	st, e = os.Stat(fpath)
	if e != nil {
		return nil, f.toClientError(e, fpath)
	}
	return st, nil
}

// Get Object Tags
func (f *fsClient) GetTags(ctx context.Context, _ string) (map[string]string, *probe.Error) {
	return nil, probe.NewError(APINotImplemented{
		API:     "GetObjectTagging",
		APIType: "filesystem",
	})
}

// Set Object tags
func (f *fsClient) SetTags(ctx context.Context, versionID, tags string) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "SetObjectTagging",
		APIType: "filesystem",
	})
}

// Delete object tags
func (f *fsClient) DeleteTags(ctx context.Context, versionID string) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "DeleteObjectTagging",
		APIType: "filesystem",
	})
}

// Get lifecycle configuration for a given bucket, not implemented.
func (f *fsClient) GetLifecycle(ctx context.Context) (*lifecycle.Configuration, *probe.Error) {
	return nil, probe.NewError(APINotImplemented{
		API:     "GetLifecycle",
		APIType: "filesystem",
	})
}

// Set lifecycle configuration for a given bucket, not implemented.
func (f *fsClient) SetLifecycle(ctx context.Context, _ *lifecycle.Configuration) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "SetLifecycle",
		APIType: "filesystem",
	})
}

// Get version info for a bucket, not implemented.
func (f *fsClient) GetVersion(ctx context.Context) (minio.BucketVersioningConfiguration, *probe.Error) {
	return minio.BucketVersioningConfiguration{}, probe.NewError(APINotImplemented{
		API:     "GetVersion",
		APIType: "filesystem",
	})
}

// SetVersion - Set version configuration on a bucket, not implemented
func (f *fsClient) SetVersion(ctx context.Context, status string) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "SetVersion",
		APIType: "filesystem",
	})
}

// Get replication configuration for a given bucket, not implemented.
func (f *fsClient) GetReplication(ctx context.Context) (replication.Config, *probe.Error) {
	return replication.Config{}, probe.NewError(APINotImplemented{
		API:     "GetReplication",
		APIType: "filesystem",
	})
}

// Set replication configuration for a given bucket, not implemented.
func (f *fsClient) SetReplication(ctx context.Context, cfg *replication.Config, opts replication.Options) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "SetReplication",
		APIType: "filesystem",
	})
}

// Remove replication configuration for a given bucket. Not implemented
func (f *fsClient) RemoveReplication(ctx context.Context) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "RemoveReplication",
		APIType: "filesystem",
	})

}

// GetReplicationMetrics - Get replication metrics for a given bucket, not implemented.
func (f *fsClient) GetReplicationMetrics(ctx context.Context) (replication.Metrics, *probe.Error) {
	return replication.Metrics{}, probe.NewError(APINotImplemented{
		API:     "GetReplicationMetrics",
		APIType: "filesystem",
	})
}

// Get encryption info for a bucket, not implemented.
func (f *fsClient) GetEncryption(ctx context.Context) (string, string, *probe.Error) {
	return "", "", probe.NewError(APINotImplemented{
		API:     "GetEncryption",
		APIType: "filesystem",
	})
}

// SetEncryption - Set encryption configuration on a bucket, not implemented
func (f *fsClient) SetEncryption(ctx context.Context, algorithm, keyID string) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "SetEncryption",
		APIType: "filesystem",
	})
}

// DeleteEncryption - removes encryption configuration on a bucket, not implemented
func (f *fsClient) DeleteEncryption(ctx context.Context) *probe.Error {
	return probe.NewError(APINotImplemented{
		API:     "DeleteEncryption",
		APIType: "filesystem",
	})
}

// Gets bucket info
func (f *fsClient) GetBucketInfo(ctx context.Context) (BucketInfo, *probe.Error) {
	return BucketInfo{}, probe.NewError(APINotImplemented{
		API:     "GetBucketInfo",
		APIType: "filesystem",
	})
}
