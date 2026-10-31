//go:build windows

// Package audio controls the default playback device through Windows Core Audio.
package audio

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	eRender                 = 0
	eMultimedia             = 1

	rpcEChangedMode = 0x80010106
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
)

var (
	clsidMMDeviceEnumerator = windows.GUID{
		Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C,
		Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E},
	}
	iidIMMDeviceEnumerator = windows.GUID{
		Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35,
		Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6},
	}
	iidIAudioEndpointVolume = windows.GUID{
		Data1: 0x5CDF2C82, Data2: 0x841E, Data3: 0x4546,
		Data4: [8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A},
	}
)

// setMasterVolumeScalar is implemented in assembly because Windows x64 passes
// the float argument in XMM1; syscall.SyscallN only supports integer registers.
// It calls IAudioEndpointVolume::SetMasterVolumeLevelScalar(fn, level, nil).
//
//go:noescape
func setMasterVolumeScalar(fn uintptr, iface unsafe.Pointer, level float32) uintptr

// SetVolumePercent sets the master volume of the default playback device.
func SetVolumePercent(percent int) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("volume must be between 0 and 100")
	}
	return withEndpointVolume(func(volume unsafe.Pointer) error {
		vtable := *(*unsafe.Pointer)(volume)
		fn := *(*uintptr)(unsafe.Add(vtable, 7*unsafe.Sizeof(uintptr(0))))
		hr := setMasterVolumeScalar(fn, volume, float32(percent)/100)
		return hresult(hr, "SetMasterVolumeLevelScalar")
	})
}

// GetVolumePercent returns the master volume of the default playback device.
func GetVolumePercent() (int, error) {
	var level float32
	err := withEndpointVolume(func(volume unsafe.Pointer) error {
		hr := call(volume, 9, uintptr(unsafe.Pointer(&level)))
		return hresult(hr, "GetMasterVolumeLevelScalar")
	})
	if err != nil {
		return 0, err
	}
	percent := int(level*100 + 0.5)
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return percent, nil
}

// ChangeVolume adjusts the master volume by delta percentage points.
func ChangeVolume(delta int) (int, error) {
	current, err := GetVolumePercent()
	if err != nil {
		return 0, err
	}
	next := current + delta
	if next < 0 {
		next = 0
	}
	if next > 100 {
		next = 100
	}
	if err := SetVolumePercent(next); err != nil {
		return current, err
	}
	if delta != 0 {
		_ = SetMuted(false)
	}
	return next, nil
}

// SetMuted mutes or unmutes the default playback device.
func SetMuted(muted bool) error {
	return withEndpointVolume(func(volume unsafe.Pointer) error {
		value := uintptr(0)
		if muted {
			value = 1
		}
		hr := call(volume, 14, value, 0) // SetMute
		return hresult(hr, "SetMute")
	})
}

func withEndpointVolume(fn func(unsafe.Pointer) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	initialized, err := comInitialize()
	if err != nil {
		return err
	}
	if initialized {
		defer procCoUninitialize.Call()
	}

	volume, err := openEndpointVolume()
	if err != nil {
		return err
	}
	defer call(volume, 2) // Release
	return fn(volume)
}

func comInitialize() (bool, error) {
	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	switch uint32(hr) {
	case 0, 1: // S_OK, S_FALSE
		return true, nil
	case rpcEChangedMode:
		// COM is already initialized in another mode; usable from this thread.
		return false, nil
	default:
		return false, hresult(hr, "CoInitializeEx")
	}
}

func openEndpointVolume() (unsafe.Pointer, error) {
	var enumerator unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if err := hresult(hr, "CoCreateInstance(MMDeviceEnumerator)"); err != nil {
		return nil, err
	}
	defer call(enumerator, 2) // Release

	var device unsafe.Pointer
	hr = call(enumerator, 4,
		uintptr(eRender),
		uintptr(eMultimedia),
		uintptr(unsafe.Pointer(&device)),
	) // GetDefaultAudioEndpoint
	if err := hresult(hr, "GetDefaultAudioEndpoint"); err != nil {
		return nil, err
	}
	defer call(device, 2) // Release

	var volume unsafe.Pointer
	hr = call(device, 3,
		uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)),
		clsctxInprocServer,
		0,
		uintptr(unsafe.Pointer(&volume)),
	) // Activate
	if err := hresult(hr, "IMMDevice.Activate"); err != nil {
		return nil, err
	}
	return volume, nil
}

func call(iface unsafe.Pointer, index int, args ...uintptr) uintptr {
	vtable := *(*unsafe.Pointer)(iface)
	fn := *(*uintptr)(unsafe.Add(vtable, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, uintptr(iface))
	callArgs = append(callArgs, args...)
	result, _, _ := syscall.SyscallN(fn, callArgs...)
	return result
}

func hresult(hr uintptr, operation string) error {
	if int32(hr) >= 0 {
		return nil
	}
	return fmt.Errorf("%s failed: HRESULT 0x%08X", operation, uint32(hr))
}
