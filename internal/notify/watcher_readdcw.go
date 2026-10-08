// Copyright (c) 2014-2020 The Notify Authors. All rights reserved.
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

//go:build windows
// +build windows

package notify

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// readBufferSize defines the size of an array in which read statuses are stored.
// The buffer have to be DWORD-aligned and, if notify is used in monitoring a
// directory over the network, its size must not be greater than 64KB. Each of
// watched directories uses its own buffer for storing events.
const readBufferSize = 4096

// Since all operations which go through the Windows completion routine are done
// asynchronously, filter may set one of the constants below. They were defined
// in order to distinguish whether current folder should be re-registered in
// ReadDirectoryChangesW function or some control operations need to be executed.
const (
	stateRewatch uint32 = 1 << (28 + iota)
	stateUnwatch
	stateCPClose
)

// Filter used in current implementation was split into four segments:
//   - bits  0-11 store ReadDirectoryChangesW filters,
//   - bits 12-19 store File notify actions,
//   - bits 20-27 store notify specific events and flags,
//   - bits 28-31 store states which are used in loop's FSM.
//
// Constants below are used as masks to retrieve only specific filter parts.
const (
	onlyNotifyChanges uint32 = 0x00000FFF
	onlyNGlobalEvents uint32 = 0x0FF00000
	onlyMachineStates uint32 = 0xF0000000
)

// grip represents a single watched directory. It stores the data required by
// ReadDirectoryChangesW function. Only the filter, recursive, and handle members
// may by modified by watcher implementation. Rest of the them have to remain
// constant since they are used by Windows completion routine. This indicates that
// grip can be removed only when all operations on the file handle are finished.
type grip struct {
	handle    syscall.Handle
	filter    uint32
	recursive bool
	pathw     []uint16
	buffer    [readBufferSize]byte
	parent    *watched
	ovlapped  *overlappedEx
}

// overlappedEx stores information used in asynchronous input and output.
// Additionally, overlappedEx contains a pointer to 'grip' item which is used in
// order to gather the structure in which the overlappedEx object was created.
type overlappedEx struct {
	syscall.Overlapped
	parent *grip
}

// newGrip creates a new file handle that can be used in overlapped operations.
// Then, the handle is associated with I/O completion port 'cph' and its value
// is stored in newly created 'grip' object.
func newGrip(cph syscall.Handle, parent *watched, filter uint32) (*grip, error) {
	g := &grip{
		handle:    syscall.InvalidHandle,
		filter:    filter,
		recursive: parent.recursive,
		pathw:     parent.pathw,
		parent:    parent,
		ovlapped:  &overlappedEx{},
	}
	g.ovlapped.parent = g
	if err := g.register(cph); err != nil {
		return nil, err
	}
	return g, nil
}

// NOTE : Thread safe
func (g *grip) register(cph syscall.Handle) (err error) {
	if g.handle, err = syscall.CreateFile(
		&g.pathw[0],
		syscall.FILE_LIST_DIRECTORY,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OVERLAPPED,
		0,
	); err != nil {
		return
	}
	// No completion owns this grip until ReadDirectoryChanges succeeds.
	// Release the opened handle on either registration or first-read failure.
	defer func() {
		if err != nil {
			syscall.CloseHandle(g.handle)
			g.handle = syscall.InvalidHandle
		}
	}()
	if _, err = syscall.CreateIoCompletionPort(g.handle, cph, 0, 0); err != nil {
		return
	}
	return g.readDirChanges()
}

// readDirChanges tells the system to store file change information in grip's
// buffer. Directory changes that occur between calls to this function are added
// to the buffer and then, returned with the next call.
func (g *grip) readDirChanges() error {
	handle := syscall.Handle(atomic.LoadUintptr((*uintptr)(&g.handle)))
	if handle == syscall.InvalidHandle {
		return nil // Handle was closed.
	}

	return syscall.ReadDirectoryChanges(
		handle,
		&g.buffer[0],
		uint32(unsafe.Sizeof(g.buffer)),
		g.recursive,
		encode(g.filter),
		nil,
		(*syscall.Overlapped)(unsafe.Pointer(g.ovlapped)),
		0,
	)
}

// encode transforms a generic filter, which contains platform independent and
// implementation specific bit fields, to value that can be used as NotifyFilter
// parameter in ReadDirectoryChangesW function.
func encode(filter uint32) uint32 {
	e := Event(filter & (onlyNGlobalEvents | onlyNotifyChanges))
	if e&dirmarker != 0 {
		return uint32(FileNotifyChangeDirName)
	}
	if e&Create != 0 {
		e = (e ^ Create) | FileNotifyChangeFileName
	}
	if e&Remove != 0 {
		e = (e ^ Remove) | FileNotifyChangeFileName
	}
	if e&Write != 0 {
		e = (e ^ Write) | FileNotifyChangeAttributes | FileNotifyChangeSize |
			FileNotifyChangeLastWrite | FileNotifyChangeCreation | FileNotifyChangeSecurity
	}
	if e&Rename != 0 {
		e = (e ^ Rename) | FileNotifyChangeFileName
	}
	return uint32(e)
}

// watched is made in order to check whether an action comes from a directory or
// file. This approach requires two file handlers per single monitored folder. The
// second grip handles actions which include creating or deleting a directory. If
// these processes are not monitored, only the first grip is created.
type watched struct {
	filter    uint32
	recursive bool
	count     uint8
	pathw     []uint16
	digrip    [2]*grip
}

// newWatched creates a new watched instance. It splits the filter variable into
// two parts. The first part is responsible for watching all events which can be
// created for a file in watched directory structure and the second one watches
// only directory Create/Remove actions. If all operations succeed, the Create
// message is sent to I/O completion port queue for further processing.
func newWatched(cph syscall.Handle, filter uint32, recursive bool,
	path string) (wd *watched, err error) {
	wd = &watched{
		filter:    filter,
		recursive: recursive,
	}
	if wd.pathw, err = syscall.UTF16FromString(path); err != nil {
		return
	}
	if err = wd.recreate(cph); err != nil {
		return
	}
	return wd, nil
}

// TODO : doc
func (wd *watched) recreate(cph syscall.Handle) (err error) {
	filefilter := wd.filter &^ uint32(FileNotifyChangeDirName)
	if err = wd.updateGrip(0, cph, filefilter == 0, filefilter); err != nil {
		return
	}
	dirfilter := wd.filter & uint32(FileNotifyChangeDirName|Create|Remove)
	if err = wd.updateGrip(1, cph, dirfilter == 0, wd.filter|uint32(dirmarker)); err != nil {
		return
	}
	wd.filter &^= onlyMachineStates
	return
}

// TODO : doc
func (wd *watched) updateGrip(idx int, cph syscall.Handle, reset bool,
	newflag uint32) (err error) {
	if reset {
		wd.digrip[idx] = nil
	} else {
		if wd.digrip[idx] == nil {
			if wd.digrip[idx], err = newGrip(cph, wd, newflag); err != nil {
				wd.closeHandle()
				return
			}
		} else {
			wd.digrip[idx].filter = newflag
			wd.digrip[idx].recursive = wd.recursive
			if err = wd.digrip[idx].register(cph); err != nil {
				wd.closeHandle()
				return
			}
		}
		wd.count++
	}
	return
}

// closeHandle retires the grip before closing its kernel handle. CloseHandle can
// queue a successful zero-byte completion before it returns; the completion must
// already see that the stream was intentionally closed. Registration and rearming
// are serialized with this operation by the watcher mutex.
func (g *grip) closeHandle(closeHandle func(syscall.Handle) error) error {
	handle := syscall.Handle(atomic.SwapUintptr((*uintptr)(&g.handle), uintptr(syscall.InvalidHandle)))
	if handle == syscall.InvalidHandle {
		return nil
	}
	// Keep the grip invalid even on failure, matching the existing close policy.
	return closeHandle(handle)
}

// closeHandle closes handles that are stored in digrip array. Function always
// tries to close all of the handlers before it exits, even when there are errors
// returned from the operating system kernel.
func (wd *watched) closeHandle() (err error) {
	for _, g := range wd.digrip {
		if g == nil {
			continue
		}

		if e := g.closeHandle(syscall.CloseHandle); e != nil && err == nil {
			err = e
		}
	}
	return
}

// watcher implements Watcher interface. It stores a set of watched directories.
// Retired watches retain their buffers until every cancelled I/O completes.
type readdcw struct {
	sync.Mutex
	m       map[string]*watched
	retired map[*watched]struct{}
	drained *sync.Cond
	cph     syscall.Handle
	start   bool
	closing bool
	wg      sync.WaitGroup
	c       chan<- EventInfo
}

// NewWatcher creates new non-recursive watcher backed by ReadDirectoryChangesW.
func newWatcher(c chan<- EventInfo) watcher {
	r := &readdcw{
		m:       make(map[string]*watched),
		retired: make(map[*watched]struct{}),
		cph:     syscall.InvalidHandle,
		c:       c,
	}
	r.drained = sync.NewCond(&r.Mutex)
	runtime.SetFinalizer(r, func(r *readdcw) {
		if r.cph != syscall.InvalidHandle {
			syscall.CloseHandle(r.cph)
		}
	})
	return r
}

// Watch implements notify.Watcher interface.
func (r *readdcw) Watch(path string, event Event) error {
	return r.watch(path, event, false)
}

// RecursiveWatch implements notify.RecursiveWatcher interface.
func (r *readdcw) RecursiveWatch(path string, event Event) error {
	return r.watch(path, event, true)
}

// watch inserts a directory to the group of watched folders. If watched folder
// already exists, function tries to rewatch it with new filters(NOT VALID). Moreover,
// watch starts the main event loop goroutine when called for the first time.
func (r *readdcw) watch(path string, event Event, recursive bool) error {
	if event&^(All|fileNotifyChangeAll) != 0 {
		return errors.New("notify: unknown event")
	}

	r.Lock()
	defer r.Unlock()
	if r.closing {
		return errors.New("notify: watcher is closing")
	}

	if wd, ok := r.m[path]; ok {
		dbgprint("watch: already exists")
		wd.filter &^= stateUnwatch
		return nil
	}

	if err := r.lazyinit(); err != nil {
		return err
	}

	wd, err := newWatched(r.cph, uint32(event), recursive, path)
	if err != nil {
		// A second grip can fail after the first read has been submitted.
		// Keep that read's memory alive until its cancellation completes.
		if wd != nil && wd.count != 0 {
			wd.filter = wd.filter&^onlyMachineStates | stateUnwatch
			r.retired[wd] = struct{}{}
		}
		return err
	}

	r.m[path] = wd
	dbgprint("watch: new watch added")

	return nil
}

// lazyinit creates an I/O completion port and starts the main event loop.
func (r *readdcw) lazyinit() (err error) {
	invalid := uintptr(syscall.InvalidHandle)

	if atomic.LoadUintptr((*uintptr)(&r.cph)) == invalid {
		cph := syscall.InvalidHandle
		if cph, err = syscall.CreateIoCompletionPort(cph, 0, 0, 0); err != nil {
			return
		}

		r.cph, r.start = cph, true
		go r.loop()
	}

	return
}

// TODO(pknap) : doc
func (r *readdcw) loop() {
	var n, key uint32
	var overlapped *syscall.Overlapped
	for {
		err := syscall.GetQueuedCompletionStatus(r.cph, &n, &key, &overlapped, syscall.INFINITE)
		if key == stateCPClose {
			r.Lock()
			handle := r.cph
			r.cph = syscall.InvalidHandle
			r.Unlock()
			syscall.CloseHandle(handle)
			r.wg.Done()
			return
		}
		if overlapped == nil {
			// TODO: check key == rewatch delete or 0(panic)
			continue
		}
		overEx := (*overlappedEx)(unsafe.Pointer(overlapped))
		if overEx == nil || overEx.parent == nil {
			dbgprintf("incomplete completion status transferred=%d, overlapped=%#v, key=%#b", n, overEx, key)
			continue
		} else {
			r.completion(n, err, overEx)
		}
		// Rearming and consuming a cancelled grip are atomic with Stop/Rewatch.
		// Otherwise Stop could see a newly armed read, but the loop would consume
		// its count for the preceding completion and free the buffer too early.
		r.Lock()
		rearmErr := overEx.parent.readDirChanges()
		stateErr := r.loopstateLocked(overEx, rearmErr != nil)
		r.Unlock()
		if rearmErr != nil || stateErr != nil {
			r.lost(overEx)
		}
	}
}

func (r *readdcw) completion(n uint32, err error, overEx *overlappedEx) {
	// Closing a directory can complete with zero bytes and no error, rather
	// than ERROR_OPERATION_ABORTED. Its stream was intentionally retired.
	if syscall.Handle(atomic.LoadUintptr((*uintptr)(&overEx.parent.handle))) == syscall.InvalidHandle {
		return
	}
	if err == syscall.ERROR_OPERATION_ABORTED {
		return // Stop/Rewatch closes handles to cancel their pending reads.
	}
	if err != nil || n == 0 {
		// A successful zero-byte completion means the kernel buffer overflowed.
		r.lost(overEx)
		return
	}
	r.loopevent(n, overEx)
}

// TODO(pknap) : doc
func (r *readdcw) loopstateLocked(overEx *overlappedEx, rearmFailed bool) error {
	wd := overEx.parent.parent
	filter := wd.filter
	if filter&onlyMachineStates == 0 && !rearmFailed {
		return nil
	}
	wd.count--
	r.drained.Broadcast()
	if wd.count == 0 {
		switch filter & onlyMachineStates {
		case stateRewatch:
			dbgprint("loopstate rewatch")
			return wd.recreate(r.cph)
		case stateUnwatch:
			dbgprint("loopstate unwatch")
			wd.closeHandle()
			delete(r.retired, wd)
			path := syscall.UTF16ToString(overEx.parent.pathw)
			if r.m[path] == wd {
				delete(r.m, path)
			}
		case stateCPClose, 0:
		default:
			panic(`notify: windows loopstate logic error`)
		}
	}
	return nil
}

// TODO(pknap) : doc
func (r *readdcw) loopevent(n uint32, overEx *overlappedEx) {
	if n > uint32(len(overEx.parent.buffer)) {
		r.lost(overEx)
		return
	}
	records, err := decodeWindowsNotifications(overEx.parent.buffer[:n])
	if err != nil {
		dbgprintf("invalid Windows notification: %v", err)
		r.lost(overEx)
		return
	}
	events := []*event{}
	for _, record := range records {
		events = append(events, &event{
			pathw:  overEx.parent.pathw,
			filter: overEx.parent.filter,
			action: record.action,
			name:   record.name,
		})
	}
	r.send(events)
}

func (r *readdcw) lost(overEx *overlappedEx) {
	r.c <- &event{pathw: overEx.parent.pathw, ftype: fTypeDirectory, e: WindowsEventsLost}
}

// TODO(pknap) : doc
func (r *readdcw) send(es []*event) {
	for _, e := range es {
		var syse Event
		if e.e, syse = decode(e.filter, e.action); e.e == 0 && syse == 0 {
			continue
		}
		switch {
		case e.action == syscall.FILE_ACTION_MODIFIED:
			e.ftype = fTypeUnknown
		case e.filter&uint32(dirmarker) != 0:
			e.ftype = fTypeDirectory
		default:
			e.ftype = fTypeFile
		}
		switch {
		case e.e == 0:
			e.e = syse
		case syse != 0:
			r.c <- &event{
				pathw:  e.pathw,
				name:   e.name,
				ftype:  e.ftype,
				action: e.action,
				filter: e.filter,
				e:      syse,
			}
		}
		r.c <- e
	}
}

// Rewatch implements notify.Rewatcher interface.
func (r *readdcw) Rewatch(path string, oldevent, newevent Event) error {
	return r.rewatch(path, uint32(oldevent), uint32(newevent), false)
}

// RecursiveRewatch implements notify.RecursiveRewatcher interface.
func (r *readdcw) RecursiveRewatch(oldpath, newpath string, oldevent,
	newevent Event) error {
	if oldpath != newpath {
		if err := r.unwatch(oldpath); err != nil {
			return err
		}
		return r.watch(newpath, newevent, true)
	}
	return r.rewatch(newpath, uint32(oldevent), uint32(newevent), true)
}

// TODO : (pknap) doc.
func (r *readdcw) rewatch(path string, oldevent, newevent uint32, recursive bool) (err error) {
	if Event(newevent)&^(All|fileNotifyChangeAll) != 0 {
		return errors.New("notify: unknown event")
	}
	var wd *watched
	r.Lock()
	defer r.Unlock()
	if wd, err = r.nonStateWatchedLocked(path); err != nil {
		return
	}
	if wd.filter&(onlyNotifyChanges|onlyNGlobalEvents) != oldevent {
		panic(`notify: windows re-watcher logic error`)
	}
	wd.filter = stateRewatch | newevent
	wd.recursive, recursive = recursive, wd.recursive
	if err = wd.closeHandle(); err != nil {
		wd.filter = oldevent
		wd.recursive = recursive
		return
	}
	return
}

// TODO : pknap
func (r *readdcw) nonStateWatchedLocked(path string) (wd *watched, err error) {
	wd, ok := r.m[path]
	if !ok || wd == nil {
		err = errors.New(`notify: ` + path + ` path is unwatched`)
		return
	}
	if wd.filter&onlyMachineStates != 0 {
		err = errors.New(`notify: another re/unwatching operation in progress`)
		return
	}
	return
}

// Unwatch implements notify.Watcher interface.
func (r *readdcw) Unwatch(path string) error {
	return r.unwatch(path)
}

// RecursiveUnwatch implements notify.RecursiveWatcher interface.
func (r *readdcw) RecursiveUnwatch(path string) error {
	return r.unwatch(path)
}

// TODO : pknap
func (r *readdcw) unwatch(path string) (err error) {
	var wd *watched

	r.Lock()
	defer r.Unlock()
	if wd, err = r.nonStateWatchedLocked(path); err != nil {
		return
	}

	wd.filter |= stateUnwatch
	dbgprint("unwatch: set unwatch state")
	// Close handles now rather than waiting for a future directory change.
	// Cancellation completions still own wd, but must not delete a new watch
	// registered at the same path before those completions are drained.
	delete(r.m, path)
	if wd.count != 0 {
		r.retired[wd] = struct{}{}
	}
	return wd.closeHandle()
}

// Close resets the whole watcher object, closes all existing file descriptors,
// and sends stateCPClose state as completion key to the main watcher's loop.
func (r *readdcw) Close() (err error) {
	r.Lock()
	if !r.start {
		r.Unlock()
		return nil
	}
	if r.closing {
		r.Unlock()
		return errors.New("notify: watcher is already closing")
	}
	r.closing = true
	for _, wd := range r.m {
		wd.filter &^= onlyMachineStates
		wd.filter |= stateCPClose
		if e := wd.closeHandle(); e != nil && err == nil {
			err = e
		}
	}
	for r.pendingLocked() {
		r.drained.Wait()
	}
	clear(r.m)
	r.start = false
	r.Unlock()
	r.wg.Add(1)
	if e := syscall.PostQueuedCompletionStatus(r.cph, 0, stateCPClose, nil); e != nil && err == nil {
		return e
	}
	r.wg.Wait()
	r.Lock()
	r.closing = false
	r.Unlock()
	return
}

func (r *readdcw) pendingLocked() bool {
	for _, wd := range r.m {
		if wd.count != 0 {
			return true
		}
	}
	return len(r.retired) != 0
}

// decode creates a notify event from both non-raw filter and action which was
// returned from completion routine. Function may return Event(0) in case when
// filter was replaced by a new value which does not contain fields that are
// valid with passed action.
func decode(filter, action uint32) (Event, Event) {
	switch action {
	case syscall.FILE_ACTION_ADDED:
		return gensys(filter, Create, FileActionAdded)
	case syscall.FILE_ACTION_REMOVED:
		return gensys(filter, Remove, FileActionRemoved)
	case syscall.FILE_ACTION_MODIFIED:
		return gensys(filter, Write, FileActionModified)
	case syscall.FILE_ACTION_RENAMED_OLD_NAME:
		return gensys(filter, Rename, FileActionRenamedOldName)
	case syscall.FILE_ACTION_RENAMED_NEW_NAME:
		return gensys(filter, Rename, FileActionRenamedNewName)
	}
	dbgprintf("cannot decode internal mask: %d", action)

	return 0, 0
}

// gensys decides whether the Windows action, system-independent event or both
// of them should be returned. Since the grip's filter may be atomically changed
// during watcher lifetime, it is possible that neither Windows nor notify masks
// are watched by the user when this function is called.
func gensys(filter uint32, ge, se Event) (gene, syse Event) {
	isdir := filter&uint32(dirmarker) != 0
	if isdir && filter&uint32(FileNotifyChangeDirName) != 0 ||
		!isdir && filter&uint32(FileNotifyChangeFileName) != 0 ||
		filter&uint32(fileNotifyChangeModified) != 0 {
		syse = se
	}
	if filter&uint32(ge) != 0 {
		gene = ge
	}
	return
}
