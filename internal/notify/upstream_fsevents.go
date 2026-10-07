//go:build darwin && !kqueue && cgo

package notify

import upstream "github.com/rjeczalik/notify"

type FSEvent = upstream.FSEvent

const (
	FSEventsMustScanSubDirs = upstream.FSEventsMustScanSubDirs
	FSEventsUserDropped     = upstream.FSEventsUserDropped
	FSEventsKernelDropped   = upstream.FSEventsKernelDropped
	FSEventsRootChanged     = upstream.FSEventsRootChanged
)
