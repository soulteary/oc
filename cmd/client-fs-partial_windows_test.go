package cmd

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestLegacyPartialWindowsFilenameLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		err    error
		want   bool
	}{
		{"long-ascii", strings.Repeat("o", 255), syscall.Errno(123), true},
		{"long-utf16", strings.Repeat("😀", 125), syscall.Errno(123), true},
		{"short-ascii", "object", syscall.Errno(123), false},
		{"utf8-is-not-utf16", strings.Repeat("界", 85), syscall.Errno(123), false},
		{"permission-error", strings.Repeat("o", 255), syscall.Errno(5), false},
		{"explicit-length-error", "object", syscall.Errno(206), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := &os.PathError{Op: "lstat", Path: tc.target + partSuffix, Err: tc.err}
			if got := legacyPartialNameTooLong(tc.target, err); got != tc.want {
				t.Fatalf("filename error classified as %v, want %v", got, tc.want)
			}
		})
	}
}
