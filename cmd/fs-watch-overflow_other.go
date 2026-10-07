//go:build !windows && (!darwin || kqueue || !cgo)

package cmd

import "github.com/soulteary/mc/internal/notify"

// Other notify backends do not expose kernel overflow reliably. Local mirror
// sources therefore also reconcile periodically, independently of this hook.
func extraFSWatchEvents() []notify.Event       { return nil }
func fsWatchNeedsRescan(notify.EventInfo) bool { return false }
