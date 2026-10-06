//go:build !unix && !windows

package cmd

import (
	"fmt"
	"os"
)

func privatePartialDirectory(os.FileInfo) bool { return false }

func securePartialDirectory(*os.Root) error {
	return fmt.Errorf("secure local staging is unsupported on this platform")
}

func renameLocalPartial(_, _ *os.Root, _ string) error {
	return fmt.Errorf("secure local staging is unsupported on this platform")
}
