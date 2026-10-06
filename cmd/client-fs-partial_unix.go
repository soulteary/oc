//go:build unix

package cmd

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func privatePartialDirectory(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0077 == 0
}

func securePartialDirectory(*os.Root) error { return nil }

// Both rename endpoints refer to opened directories, not replaceable paths.
func renameLocalPartial(stage, parent *os.Root, target string) error {
	source, err := stage.Open(".")
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer destination.Close()
	err = unix.Renameat(int(source.Fd()), partialDataName, int(destination.Fd()), target)
	if err != nil {
		return &os.LinkError{Op: "renameat", Old: partialDataName, New: target, Err: err}
	}
	return nil
}
