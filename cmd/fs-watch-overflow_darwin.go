//go:build darwin && !kqueue && cgo

package cmd

import "github.com/soulteary/mc/internal/notify"

const fsWatchNativeLoss = notify.FSEventsMustScanSubDirs | notify.FSEventsUserDropped | notify.FSEventsKernelDropped | notify.FSEventsRootChanged

func extraFSWatchEvents() []notify.Event { return []notify.Event{fsWatchNativeLoss} }
func fsWatchNeedsRescan(event notify.EventInfo) bool {
	if native, ok := event.Sys().(*notify.FSEvent); ok {
		return native.Flags&uint32(fsWatchNativeLoss) != 0
	}
	return event.Event()&fsWatchNativeLoss != 0
}
