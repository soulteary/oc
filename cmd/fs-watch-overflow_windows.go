package cmd

import "github.com/soulteary/mc/internal/notify"

func extraFSWatchEvents() []notify.Event { return nil }
func fsWatchNeedsRescan(event notify.EventInfo) bool {
	return event.Event()&notify.WindowsEventsLost != 0
}
