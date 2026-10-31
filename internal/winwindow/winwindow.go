// Package winwindow exposes the small subset of Win32 window queries the
// assistant needs. Wails does not report whether its window is currently
// visible, so the cached UI state cannot be trusted after the close button
// hides the window.
package winwindow

import (
	"syscall"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	findWindowW     = user32.NewProc("FindWindowW")
	isWindowVisible = user32.NewProc("IsWindowVisible")
)

// IsVisible reports whether a top-level window with the exact title exists and
// is visible.
func IsVisible(title string) bool {
	pointer, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return false
	}
	window, _, _ := findWindowW.Call(0, uintptr(unsafe.Pointer(pointer)))
	if window == 0 {
		return false
	}
	visible, _, _ := isWindowVisible.Call(window)
	return visible != 0
}
