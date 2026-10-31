// Package hotkey registers a single global hotkey on Windows.
package hotkey

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32             = syscall.NewLazyDLL("user32.dll")
	registerHotKey     = user32.NewProc("RegisterHotKey")
	unregisterHotKey   = user32.NewProc("UnregisterHotKey")
	peekMessage        = user32.NewProc("PeekMessageW")
	waitMessage        = user32.NewProc("WaitMessage")
	postThreadMessage  = user32.NewProc("PostThreadMessageW")
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	getCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

const (
	// MOD_ALT is the Alt modifier.
	MOD_ALT = 0x0001
	// MOD_CONTROL is the Ctrl modifier.
	MOD_CONTROL = 0x0002
	// MOD_SHIFT is the Shift modifier.
	MOD_SHIFT = 0x0004
	// MOD_WIN is the Windows key modifier.
	MOD_WIN = 0x0008
	// MOD_NOREPEAT prevents key-repeat storms.
	MOD_NOREPEAT = 0x4000

	WM_HOTKEY = 0x0312
	WM_NULL   = 0x0000

	// VK_J is the J key.
	VK_J = 0x4A
)

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
}

var (
	mu       sync.Mutex
	callback func()
	running  atomic.Bool
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
	threadID uint32
)

// Start registers the hotkey and starts the message loop. It returns an error
// when the combination is already taken by another application.
func Start(id int, mods, vk uintptr, onPress func()) error {
	mu.Lock()
	if running.Load() {
		mu.Unlock()
		return fmt.Errorf("hotkey listener already running")
	}
	callback = onPress
	stopCh = make(chan struct{})
	doneCh = make(chan struct{})
	stopOnce = sync.Once{}
	running.Store(true)
	mu.Unlock()

	result := make(chan error, 1)
	go listenLoop(id, mods, vk, stopCh, doneCh, result)

	if err := <-result; err != nil {
		running.Store(false)
		return err
	}
	return nil
}

// Register binds the hotkey to the current thread.
func Register(id int, mods, vk uintptr) error {
	ret, _, err := registerHotKey.Call(0, uintptr(id), mods, vk)
	if ret == 0 {
		return fmt.Errorf("RegisterHotKey failed: %w", err)
	}
	return nil
}

// Unregister releases the hotkey from the calling thread.
func Unregister(id int) {
	unregisterHotKey.Call(0, uintptr(id))
}

// Stop terminates the message loop and releases the hotkey. Safe to call twice.
func Stop() {
	running.Store(false)

	mu.Lock()
	channel := stopCh
	done := doneCh
	thread := threadID
	mu.Unlock()

	if channel != nil {
		stopOnce.Do(func() { close(channel) })
	}
	if thread != 0 {
		postThreadMessage.Call(uintptr(thread), WM_NULL, 0, 0)
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			// Do not hang application shutdown if the message queue could not
			// be woken up; Windows will release the thread on exit.
		}
	}
}

func listenLoop(id int, mods, vk uintptr, stop <-chan struct{}, done chan<- struct{}, result chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(done)

	thread, _, _ := getCurrentThreadID.Call()
	mu.Lock()
	threadID = uint32(thread)
	mu.Unlock()

	// RegisterHotKey associates the hotkey with the calling thread, so it must
	// run on the same locked thread as the message loop.
	if err := Register(id, mods, vk); err != nil {
		mu.Lock()
		if threadID == uint32(thread) {
			threadID = 0
		}
		mu.Unlock()
		result <- err
		return
	}
	result <- nil
	defer func() {
		Unregister(id)
		mu.Lock()
		if threadID == uint32(thread) {
			threadID = 0
		}
		mu.Unlock()
	}()

	var message msg
	for running.Load() {
		select {
		case <-stop:
			return
		default:
		}

		waitMessage.Call()

		for {
			ret, _, _ := peekMessage.Call(
				uintptr(unsafe.Pointer(&message)),
				0,
				WM_HOTKEY,
				WM_HOTKEY,
				1,
			)
			if ret == 0 {
				break
			}
			if message.Message == WM_HOTKEY && int(message.WParam) == id {
				mu.Lock()
				handler := callback
				mu.Unlock()
				if handler != nil {
					go handler()
				}
			}
		}
	}
}
