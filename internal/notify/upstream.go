//go:build !windows

// Package notify keeps OC's notification types consistent across platforms.
// Non-Windows platforms use the unmodified upstream implementation.
package notify

import upstream "github.com/rjeczalik/notify"

type Event = upstream.Event
type EventInfo = upstream.EventInfo

const (
	Create = upstream.Create
	Remove = upstream.Remove
	Write  = upstream.Write
	Rename = upstream.Rename
)

func Watch(path string, c chan<- EventInfo, events ...Event) error {
	return upstream.Watch(path, c, events...)
}

func Stop(c chan<- EventInfo) { upstream.Stop(c) }
