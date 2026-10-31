//go:build windows

package setup

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	relationProcessorCore = 0
	dxgiErrorNotFound     = 0x887A0002
	dxgiAdapterFlagSoft   = 0x2

	// IUnknown::Release is at index 2. These are the vtable slots after the
	// IUnknown and IDXGIObject methods.
	dxgiRelease              = 2
	dxgiAdapterGetDesc1      = 10 // IDXGIAdapter1::GetDesc1
	dxgiFactoryEnumAdapters1 = 12 // IDXGIFactory1::EnumAdapters1
)

type dxgiAdapterDesc1 struct {
	Description           [128]uint16
	VendorID              uint32
	DeviceID              uint32
	SubSysID              uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	AdapterLUID           uint64
	Flags                 uint32
}

func applyHardwareInfo(info *SystemInfo) {
	name, cores, threads, clockGHz := readCPUInfo()
	info.CPUName = name
	info.CPUCores = cores
	info.CPUThreads = threads
	info.CPUClockGHz = clockGHz

	gpus := detectGPUs()
	if len(gpus) > 0 {
		info.GPUs = gpus
		info.GPUName = gpus[0].Name
		info.GPUVendor = gpus[0].Vendor
		info.GPUVRAMGB = gpus[0].VRAMGB
		info.GPUIntegrated = gpus[0].Integrated
	}

	freeGB, totalGB := diskSpace()
	info.DiskFreeGB = freeGB
	info.DiskTotalGB = totalGB
}

func readCPUInfo() (string, int, int, float64) {
	name := "Unknown CPU"
	var clockGHz float64
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`,
		registry.QUERY_VALUE)
	if err == nil {
		if value, _, valueErr := key.GetStringValue("ProcessorNameString"); valueErr == nil {
			name = strings.TrimSpace(value)
		}
		if mhz, _, valueErr := key.GetIntegerValue("~MHz"); valueErr == nil && mhz > 0 {
			clockGHz = float64(mhz) / 1000
		}
		_ = key.Close()
	}

	threads := runtime.NumCPU()
	cores := physicalCoreCount()
	if cores <= 0 {
		cores = (threads + 1) / 2
	}
	if threads <= 0 {
		threads = cores
	}
	return name, cores, threads, clockGHz
}

func physicalCoreCount() int {
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetLogicalProcessorInformationEx")
	if err := proc.Find(); err != nil {
		return 0
	}

	var length uint32
	_, _, _ = proc.Call(relationProcessorCore, 0, uintptr(unsafe.Pointer(&length)))
	if length == 0 {
		return 0
	}

	buffer := make([]byte, length)
	result, _, _ := proc.Call(
		relationProcessorCore,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&length)),
	)
	if result == 0 {
		return 0
	}

	count := 0
	for offset := uintptr(0); offset+8 <= uintptr(len(buffer)); {
		relationship := *(*uint32)(unsafe.Pointer(&buffer[offset]))
		size := *(*uint32)(unsafe.Pointer(&buffer[offset+4]))
		if relationship == relationProcessorCore {
			count++
		}
		if size < 8 || offset+uintptr(size) > uintptr(len(buffer)) {
			break
		}
		offset += uintptr(size)
	}
	return count
}

func diskSpace() (int, int) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("SystemDrive") + `\`
	}
	if home == "" || home == `\` {
		home = `C:\`
	}
	root := filepath.VolumeName(home)
	if root == "" {
		root = `C:`
	}
	root += `\`

	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")
	if err := proc.Find(); err != nil {
		return 0, 0
	}
	path, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, 0
	}

	var freeAvailable, totalBytes, totalFree uint64
	result, _, _ := proc.Call(
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&freeAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if result == 0 {
		return 0, 0
	}
	const gib = 1024 * 1024 * 1024
	return int(freeAvailable / gib), int(totalBytes / gib)
}

func detectGPUs() []GPUInfo {
	if gpus := detectGPUsDXGI(); len(gpus) > 0 {
		return gpus
	}
	return detectGPUsRegistry()
}

func detectGPUsDXGI() []GPUInfo {
	dll := windows.NewLazySystemDLL("dxgi.dll")
	proc := dll.NewProc("CreateDXGIFactory1")
	if err := proc.Find(); err != nil {
		return nil
	}

	factoryIID := windows.GUID{
		Data1: 0x770AAE78,
		Data2: 0xF26F,
		Data3: 0x4DBA,
		Data4: [8]byte{0xA8, 0x29, 0x25, 0x3C, 0x83, 0xD1, 0xB3, 0x87},
	}
	var factory unsafe.Pointer
	result, _, _ := proc.Call(
		uintptr(unsafe.Pointer(&factoryIID)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if int32(result) < 0 || factory == nil {
		return nil
	}
	defer comCall(factory, dxgiRelease) // IUnknown::Release

	var gpus []GPUInfo
	for index := 0; ; index++ {
		var adapter unsafe.Pointer
		result, _, _ = comCallResult(factory, dxgiFactoryEnumAdapters1, uintptr(index), uintptr(unsafe.Pointer(&adapter)))
		if uint32(result) == dxgiErrorNotFound || int32(result) < 0 {
			break
		}
		if adapter == nil {
			break
		}

		var desc dxgiAdapterDesc1
		descResult, _, _ := comCallResult(adapter, dxgiAdapterGetDesc1, uintptr(unsafe.Pointer(&desc)))
		if int32(descResult) >= 0 {
			name := strings.TrimSpace(windows.UTF16ToString(desc.Description[:]))
			if name != "" && desc.Flags&dxgiAdapterFlagSoft == 0 && desc.VendorID != 0x1414 {
				vram := float64(desc.DedicatedVideoMemory) / (1024 * 1024 * 1024)
				gpus = append(gpus, GPUInfo{
					Name:       name,
					Vendor:     gpuVendor(desc.VendorID),
					VRAMGB:     roundGB(vram),
					Integrated: vram < 1.0,
				})
			}
		}
		comCall(adapter, dxgiRelease) // IUnknown::Release
	}

	sort.SliceStable(gpus, func(i, j int) bool { return gpus[i].VRAMGB > gpus[j].VRAMGB })
	return gpus
}

func detectGPUsRegistry() []GPUInfo {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`,
		registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()

	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var gpus []GPUInfo
	for _, subkey := range names {
		child, childErr := registry.OpenKey(key, subkey, registry.QUERY_VALUE)
		if childErr != nil {
			continue
		}
		name, _, _ := child.GetStringValue("DriverDesc")
		provider, _, _ := child.GetStringValue("ProviderName")
		memory, _, _ := child.GetIntegerValue("HardwareInformation.qwMemorySize")
		_ = child.Close()

		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		vram := roundGB(float64(memory) / (1024 * 1024 * 1024))
		gpus = append(gpus, GPUInfo{
			Name:       name,
			Vendor:     vendorFromProvider(provider),
			VRAMGB:     vram,
			Integrated: vram < 1.0,
		})
	}
	sort.SliceStable(gpus, func(i, j int) bool { return gpus[i].VRAMGB > gpus[j].VRAMGB })
	return gpus
}

func comCall(this unsafe.Pointer, index uintptr, args ...uintptr) uintptr {
	result, _, _ := comCallResult(this, index, args...)
	return result
}

func comCallResult(this unsafe.Pointer, index uintptr, args ...uintptr) (uintptr, uintptr, error) {
	vtable := *(*unsafe.Pointer)(this)
	fn := *(*uintptr)(unsafe.Add(vtable, index*unsafe.Sizeof(uintptr(0))))
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, uintptr(this))
	callArgs = append(callArgs, args...)
	return syscall.SyscallN(fn, callArgs...)
}

func gpuVendor(vendorID uint32) string {
	switch vendorID {
	case 0x10DE:
		return "NVIDIA"
	case 0x1002, 0x1022:
		return "AMD"
	case 0x8086:
		return "Intel"
	default:
		return "Other"
	}
}

func vendorFromProvider(provider string) string {
	lower := strings.ToLower(provider)
	switch {
	case strings.Contains(lower, "nvidia"):
		return "NVIDIA"
	case strings.Contains(lower, "amd"), strings.Contains(lower, "advanced micro"):
		return "AMD"
	case strings.Contains(lower, "intel"):
		return "Intel"
	default:
		return "Other"
	}
}

func roundGB(value float64) float64 {
	if value <= 0 {
		return 0
	}
	return float64(int(value*10+0.5)) / 10
}
