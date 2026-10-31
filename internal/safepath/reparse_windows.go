//go:build windows

package safepath

import (
	"os"
	"syscall"
)

const fileAttributeReparsePoint = 0x400

// isReparsePoint reports whether the file is a symlink, junction, or another
// reparse point. filepath.EvalSymlinks does not resolve directory junctions on
// Windows, so they must be rejected before a missing descendant is appended.
func isReparsePoint(info os.FileInfo) bool {
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return data.FileAttributes&fileAttributeReparsePoint != 0
	}
	return info.Mode()&os.ModeSymlink != 0
}
