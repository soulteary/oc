package cmd

import (
	"os"

	"github.com/mattn/go-isatty"
)

// Shared by cat and head; terminal detection is independent of self-update.
func isTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stderr.Fd())
}
