// Package setup detects hardware and Ollama availability.
package setup

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// GPUInfo describes one graphics adapter.
type GPUInfo struct {
	Name       string  `json:"name"`
	Vendor     string  `json:"vendor"`
	VRAMGB     float64 `json:"vram_gb"`
	Integrated bool    `json:"integrated"`
}

// SystemInfo describes the current machine.
type SystemInfo struct {
	RAMGB           int       `json:"ram_gb"`
	OS              string    `json:"os"`
	Arch            string    `json:"arch"`
	CPUName         string    `json:"cpu_name"`
	CPUCores        int       `json:"cpu_cores"`
	CPUThreads      int       `json:"cpu_threads"`
	CPUClockGHz     float64   `json:"cpu_clock_ghz"`
	GPUName         string    `json:"gpu_name"`
	GPUVendor       string    `json:"gpu_vendor"`
	GPUVRAMGB       float64   `json:"gpu_vram_gb"`
	GPUIntegrated   bool      `json:"gpu_integrated"`
	GPUs            []GPUInfo `json:"gpus,omitempty"`
	DiskFreeGB      int       `json:"disk_free_gb"`
	DiskTotalGB     int       `json:"disk_total_gb"`
	OllamaInstalled bool      `json:"ollama_installed"`
	OllamaRunning   bool      `json:"ollama_running"`
}

// ModelRecommendation is a suggested Ollama model.
type ModelRecommendation struct {
	Model            string  `json:"model"`
	Size             string  `json:"size"`
	DownloadGB       float64 `json:"download_gb"`
	RAMRequired      int     `json:"ram_required"`
	VRAMRequiredGB   float64 `json:"vram_required_gb"`
	CPUCoresRequired int     `json:"cpu_cores_required"`
	Device           string  `json:"device"`
	Description      string  `json:"description"`
	PullCommand      string  `json:"pull_command"`
}

// DetectSystem inspects hardware and Ollama without blocking for long.
func DetectSystem() (*SystemInfo, error) {
	info := &SystemInfo{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}
	if ram, err := TotalRAMGB(); err == nil {
		info.RAMGB = ram
	}
	applyHardwareInfo(info)
	info.OllamaInstalled = OllamaInstalled()
	info.OllamaRunning = ollamaRunning()
	return info, nil
}

// TotalRAMGB returns installed physical memory in gigabytes.
func TotalRAMGB() (int, error) {
	if runtime.GOOS != "windows" {
		return 0, fmt.Errorf("unsupported OS")
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GlobalMemoryStatusEx")
	var memory memoryStatusEx
	memory.Length = uint32(unsafe.Sizeof(memory))
	ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&memory)))
	if ret == 0 {
		return 0, fmt.Errorf("GlobalMemoryStatusEx failed")
	}
	return int(memory.TotalPhys / (1024 * 1024 * 1024)), nil
}

// OllamaInstalled reports whether the ollama executable is reachable.
func OllamaInstalled() bool {
	return OllamaPath() != ""
}

// OllamaPath returns the ollama executable path, checking PATH and the default
// per-user install locations.
func OllamaPath() string {
	if path, err := exec.LookPath("ollama"); err == nil {
		return path
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	candidates := make([]string, 0, 4)
	if localAppData != "" {
		candidates = append(candidates,
			filepath.Join(localAppData, "Programs", "Ollama", "ollama.exe"),
			filepath.Join(localAppData, "Ollama", "ollama.exe"),
		)
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		candidates = append(candidates, filepath.Join(programFiles, "Ollama", "ollama.exe"))
	}
	if programFilesX86 := os.Getenv("ProgramFiles(x86)"); programFilesX86 != "" {
		candidates = append(candidates, filepath.Join(programFilesX86, "Ollama", "ollama.exe"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func ollamaRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:11434/api/tags", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

// OllamaDownloadURL returns the official download page.
func OllamaDownloadURL() string {
	switch runtime.GOOS {
	case "windows":
		return "https://ollama.com/download/windows"
	case "darwin":
		return "https://ollama.com/download/macOS"
	default:
		return "https://ollama.com/download"
	}
}
