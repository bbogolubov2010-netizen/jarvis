package jobobject

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32.NewProc("Process32NextW")
	procOpenProcess              = kernel32.NewProc("OpenProcess")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
)

const (
	jobObjectLimitKillOnJobClose    = 0x2000
	jobObjectInfoClassExtendedLimit = 9
	processSetQuota                 = 0x0100
	processTerminate                = 0x0001
	th32csSnapProcess               = 0x00000002
	maxPath                         = 260
	maxPasses                       = 8
)

type processEntry32 struct {
	dwSize              uint32
	cntUsage            uint32
	th32ProcessID       uint32
	th32DefaultHeapID   uintptr
	th32ModuleID        uint32
	cntThreads          uint32
	th32ParentProcessID uint32
	pcPriClassBase      int32
	dwFlags             uint32
	szExeFile           [maxPath]uint16
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation struct {
		PerProcessUserTimeLimit int64
		PerJobUserTimeLimit     int64
		LimitFlags              uint32
		MinimumWorkingSetSize   uintptr
		MaximumWorkingSetSize   uintptr
		ActiveProcessLimit      uint32
		Affinity                uintptr
		ChildProcessRateControl uint32
		_                       [4]byte
	}
	IoInfo struct {
		ReadOperationCount  int64
		WriteOperationCount int64
		OtherOperationCount int64
		ReadTransferCount   int64
		WriteTransferCount  int64
		OtherTransferCount  int64
	}
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// Handle wraps a Windows job object.
type Handle struct {
	handle windows.Handle
}

// Create builds a job object that kills its processes when closed. No process
// limit is set: Chromium legitimately uses many processes.
func Create() (*Handle, error) {
	handle, _, err := procCreateJobObjectW.Call(0, 0)
	if handle == 0 {
		return nil, fmt.Errorf("CreateJobObject failed: %w", err)
	}
	job := &Handle{handle: windows.Handle(handle)}

	info := &jobObjectExtendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose

	ret, _, err := procSetInformationJobObject.Call(
		uintptr(job.handle),
		jobObjectInfoClassExtendedLimit,
		uintptr(unsafe.Pointer(info)),
		uintptr(unsafe.Sizeof(*info)),
	)
	if ret == 0 {
		procCloseHandle.Call(uintptr(job.handle))
		return nil, fmt.Errorf("SetInformationJobObject failed: %w", err)
	}
	return job, nil
}

// AssignChildProcesses assigns chrome.exe descendants of this process to the
// job. Several passes are needed because Chromium spawns processes in waves.
func (j *Handle) AssignChildProcesses() error {
	parentPID := uint32(os.Getpid())
	assigned := map[uint32]bool{parentPID: true}
	var lastErr error

	for pass := 0; pass < maxPasses; pass++ {
		progress := false

		snapshot, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
		if snapshot == 0 || snapshot == ^uintptr(0) {
			return fmt.Errorf("CreateToolhelp32Snapshot failed")
		}

		entry := &processEntry32{}
		entry.dwSize = uint32(unsafe.Sizeof(*entry))

		ret, _, _ := procProcess32FirstW.Call(snapshot, uintptr(unsafe.Pointer(entry)))
		for ret != 0 {
			pid := entry.th32ProcessID
			if !assigned[pid] && assigned[entry.th32ParentProcessID] {
				exeName := windows.UTF16ToString(entry.szExeFile[:])
				if strings.EqualFold(exeName, "chrome.exe") {
					if err := j.assign(pid); err != nil {
						lastErr = err
					} else {
						assigned[pid] = true
						progress = true
					}
				}
			}
			ret, _, _ = procProcess32NextW.Call(snapshot, uintptr(unsafe.Pointer(entry)))
		}
		procCloseHandle.Call(snapshot)

		if !progress {
			break
		}
	}

	if lastErr != nil && len(assigned) == 1 {
		return lastErr
	}
	return nil
}

func (j *Handle) assign(pid uint32) error {
	process, _, err := procOpenProcess.Call(
		processSetQuota|processTerminate,
		0,
		uintptr(pid),
	)
	if process == 0 {
		return fmt.Errorf("OpenProcess(%d) failed: %w", pid, err)
	}
	defer procCloseHandle.Call(process)

	ret, _, err := procAssignProcessToJobObject.Call(uintptr(j.handle), process)
	if ret == 0 {
		return fmt.Errorf("AssignProcessToJobObject(%d) failed: %w", pid, err)
	}
	return nil
}

// Close releases the job handle; assigned processes are terminated.
func (j *Handle) Close() {
	if j.handle != 0 {
		procCloseHandle.Call(uintptr(j.handle))
		j.handle = 0
	}
}
