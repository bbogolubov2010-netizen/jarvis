// Package tray creates the Windows notification-area icon and its menu.
package tray

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// Callbacks are invoked from the tray message loop.
type Callbacks struct {
	OnToggle func()
	OnQuit   func()
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	createWindowExW        = user32.NewProc("CreateWindowExW")
	defWindowProcW         = user32.NewProc("DefWindowProcW")
	registerClassExW       = user32.NewProc("RegisterClassExW")
	getMessageW            = user32.NewProc("GetMessageW")
	dispatchMessageW       = user32.NewProc("DispatchMessageW")
	postMessageW           = user32.NewProc("PostMessageW")
	postQuitMessage        = user32.NewProc("PostQuitMessage")
	destroyIcon            = user32.NewProc("DestroyIcon")
	createIconFromResource = user32.NewProc("CreateIconFromResource")
	setForegroundWindow    = user32.NewProc("SetForegroundWindow")
	createPopupMenu        = user32.NewProc("CreatePopupMenu")
	appendMenuW            = user32.NewProc("AppendMenuW")
	trackPopupMenu         = user32.NewProc("TrackPopupMenu")
	destroyMenu            = user32.NewProc("DestroyMenu")
	getCursorPos           = user32.NewProc("GetCursorPos")

	shellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	getModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

var (
	clsName   = utf16("JarvisTrayWindow")
	wndName   = utf16("JARVIS Tray")
	className = &clsName[0]
)

var (
	stateMu    sync.Mutex
	hwnd       uintptr
	hIcon      uintptr
	toggleCB   func()
	quitCB     func()
	notifyIcon NotifyIconDataV2
)

const (
	NIM_ADD     = 0
	NIM_DELETE  = 2
	NIF_MESSAGE = 1
	NIF_ICON    = 2
	NIF_TIP     = 4

	WM_USER  = 0x0400
	WM_TRAY  = WM_USER + 1
	WM_CLOSE = 0x0010
	WM_NULL  = 0x0000

	WM_LBUTTONUP = 0x0202
	WM_RBUTTONUP = 0x0205

	MF_STRING       = 0x0000
	TPM_RIGHTBUTTON = 0x0002
	TPM_BOTTOMALIGN = 0x0020
	TPM_RETURNCMD   = 0x0100

	menuShow = 1
	menuQuit = 2
)

// WndClassEx mirrors WNDCLASSEXW.
type WndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

// NotifyIconDataV2 mirrors the legacy NOTIFYICONDATAW layout used by the shell.
type NotifyIconDataV2 struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
}

type Msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
}

type point struct {
	X int32
	Y int32
}

func trayProc(hw uintptr, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAY:
		switch uint32(lParam) & 0xFFFF {
		case WM_LBUTTONUP:
			if toggleCB != nil {
				go toggleCB()
			}
		case WM_RBUTTONUP:
			showMenu()
		}
		return 0
	case WM_CLOSE:
		postQuitMessage.Call(0)
		return 0
	default:
		ret, _, _ := defWindowProcW.Call(hw, uintptr(msg), wParam, lParam)
		return ret
	}
}

func showMenu() {
	stateMu.Lock()
	menuWindow := hwnd
	stateMu.Unlock()
	if menuWindow == 0 {
		return
	}
	menu, _, _ := createPopupMenu.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)

	showText := utf16("Show / Hide")
	appendMenuW.Call(menu, MF_STRING, menuShow, uintptr(unsafe.Pointer(&showText[0])))
	quitText := utf16("Quit JARVIS")
	appendMenuW.Call(menu, MF_STRING, menuQuit, uintptr(unsafe.Pointer(&quitText[0])))
	appendMenuW.Call(menu, 0x0800, 0, 0) // MF_SEPARATOR

	var cursor point
	getCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	setForegroundWindow.Call(menuWindow)

	command, _, _ := trackPopupMenu.Call(
		menu,
		TPM_RIGHTBUTTON|TPM_BOTTOMALIGN|TPM_RETURNCMD,
		uintptr(cursor.X),
		uintptr(cursor.Y),
		0,
		menuWindow,
		0,
	)
	postMessageW.Call(menuWindow, WM_NULL, 0, 0)

	switch command {
	case menuShow:
		if toggleCB != nil {
			go toggleCB()
		}
	case menuQuit:
		if quitCB != nil {
			go quitCB()
		}
	}

	runtime.KeepAlive(showText)
	runtime.KeepAlive(quitText)
}

// Run creates the icon and blocks in the message loop.
func Run(callbacks Callbacks) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	stateMu.Lock()
	toggleCB = callbacks.OnToggle
	quitCB = callbacks.OnQuit
	stateMu.Unlock()

	instance, _, _ := getModuleHandleW.Call(0)

	iconData := generateICO()
	imgData := iconData[22:]
	icon, _, _ := createIconFromResource.Call(
		uintptr(unsafe.Pointer(&imgData[0])),
		uintptr(len(imgData)),
		1,
		0x00030000,
	)
	if icon == 0 {
		loadIcon := user32.NewProc("LoadIconW")
		icon, _, _ = loadIcon.Call(0, 32512)
	}
	if icon == 0 {
		fmt.Fprintln(os.Stderr, "tray: could not create an icon")
		return
	}

	stateMu.Lock()
	hIcon = icon
	stateMu.Unlock()

	wc := WndClassEx{
		CbSize:        uint32(unsafe.Sizeof(WndClassEx{})),
		LpfnWndProc:   syscall.NewCallback(trayProc),
		HInstance:     instance,
		LpszClassName: className,
	}
	registerClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	created, _, _ := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(&wndName[0])),
		0,
		0, 0, 0, 0,
		0, 0, instance, 0,
	)
	if created == 0 {
		fmt.Fprintln(os.Stderr, "tray: could not create a message window")
		destroyIcon.Call(icon)
		stateMu.Lock()
		hIcon = 0
		stateMu.Unlock()
		return
	}

	stateMu.Lock()
	hwnd = created
	stateMu.Unlock()

	iconDataStruct := NotifyIconDataV2{
		CbSize:           uint32(unsafe.Sizeof(NotifyIconDataV2{})),
		HWnd:             created,
		UID:              1,
		UFlags:           NIF_MESSAGE | NIF_ICON | NIF_TIP,
		UCallbackMessage: WM_TRAY,
		HIcon:            icon,
	}
	copy(iconDataStruct.SzTip[:], utf16("JARVIS - AI Assistant"))

	added, _, _ := shellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&iconDataStruct)))
	if added == 0 {
		fmt.Fprintln(os.Stderr, "tray: Shell_NotifyIcon(NIM_ADD) failed")
		destroyIcon.Call(icon)
		postQuitMessage.Call(0)
		stateMu.Lock()
		hIcon = 0
		hwnd = 0
		stateMu.Unlock()
		return
	}

	stateMu.Lock()
	notifyIcon = iconDataStruct
	stateMu.Unlock()

	var message Msg
	for {
		ret, _, _ := getMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if ret == 0 {
			break
		}
		dispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
}

// Stop removes the icon and closes the message loop.
func Stop() {
	stateMu.Lock()
	window := hwnd
	icon := hIcon
	iconData := notifyIcon
	hwnd = 0
	hIcon = 0
	notifyIcon = NotifyIconDataV2{}
	stateMu.Unlock()

	if window != 0 && iconData.CbSize != 0 {
		shellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&iconData)))
	}
	if window != 0 {
		postMessageW.Call(window, WM_CLOSE, 0, 0)
	}
	if icon != 0 {
		destroyIcon.Call(icon)
	}
}

func utf16(s string) []uint16 {
	result := make([]uint16, len(s)+1)
	for index, char := range s {
		result[index] = uint16(char)
	}
	return result
}

// generateICO returns a valid .ico file: 16x16 32bpp, cyan circle (#00D4FF).
func generateICO() []byte {
	const width, height = 16, 16
	const bitsPerPixel = uint16(32)
	xorSize := width * height * 4
	andRowBytes := ((width + 31) / 32) * 4
	andSize := height * andRowBytes
	imgSize := 40 + xorSize + andSize

	buffer := make([]byte, 0, 22+imgSize)

	buffer = append(buffer, 0, 0)
	buffer = append(buffer, 1, 0)
	buffer = append(buffer, 1, 0)

	buffer = append(buffer, byte(width), byte(height), 0, 0)
	buffer = append(buffer, 1, 0)
	buffer = append(buffer, byte(bitsPerPixel), 0)
	writeLE(&buffer, uint32(imgSize))
	writeLE(&buffer, uint32(22))

	writeLE(&buffer, uint32(40))
	writeLE(&buffer, uint32(width))
	writeLE(&buffer, uint32(height*2))
	writeLE(&buffer, uint16(1))
	writeLE(&buffer, bitsPerPixel)
	buffer = append(buffer, make([]byte, 24)...)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			centerX := float64(x) - float64(width)/2 + 0.5
			centerY := float64(y) - float64(height)/2 + 0.5
			if centerX*centerX+centerY*centerY <= 49 {
				buffer = append(buffer, 0xFF, 0xD4, 0x00, 0xFF)
			} else {
				buffer = append(buffer, 0, 0, 0, 0)
			}
		}
	}

	buffer = append(buffer, make([]byte, andSize)...)
	return buffer
}

func writeLE(buffer *[]byte, value any) {
	switch typed := value.(type) {
	case uint32:
		var tmp [4]byte
		binary.LittleEndian.PutUint32(tmp[:], typed)
		*buffer = append(*buffer, tmp[:]...)
	case uint16:
		var tmp [2]byte
		binary.LittleEndian.PutUint16(tmp[:], typed)
		*buffer = append(*buffer, tmp[:]...)
	}
}
