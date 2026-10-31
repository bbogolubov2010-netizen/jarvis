package setup

import (
	"fmt"
	"math"
	"strings"
)

// Model catalog. Tags are pinned to real Ollama library models so the
// recommendation can always be pulled.
var modelCatalog = []ModelRecommendation{
	{
		Model:            "llama3.2:1b",
		Size:             "1.3 GB",
		DownloadGB:       1.3,
		RAMRequired:      4,
		VRAMRequiredGB:   2,
		CPUCoresRequired: 2,
		Description:      "Fastest option for very small or older machines",
	},
	{
		Model:            "llama3.2:3b",
		Size:             "2.0 GB",
		DownloadGB:       2.0,
		RAMRequired:      6,
		VRAMRequiredGB:   3.5,
		CPUCoresRequired: 2,
		Description:      "Fast daily assistant with a small memory footprint",
	},
	{
		Model:            "llama3.1:8b",
		Size:             "4.9 GB",
		DownloadGB:       4.9,
		RAMRequired:      10,
		VRAMRequiredGB:   6.5,
		CPUCoresRequired: 4,
		Description:      "Good balance of speed, reasoning and coding",
	},
	{
		Model:            "qwen2.5:14b",
		Size:             "9.0 GB",
		DownloadGB:       9.0,
		RAMRequired:      18,
		VRAMRequiredGB:   11,
		CPUCoresRequired: 6,
		Description:      "Strong reasoning and coding for larger systems",
	},
	{
		Model:            "qwen2.5:32b",
		Size:             "20 GB",
		DownloadGB:       20.0,
		RAMRequired:      36,
		VRAMRequiredGB:   21,
		CPUCoresRequired: 8,
		Description:      "High quality local intelligence for powerful machines",
	},
	{
		Model:            "qwen2.5:72b",
		Size:             "43 GB",
		DownloadGB:       43.0,
		RAMRequired:      72,
		VRAMRequiredGB:   46,
		CPUCoresRequired: 12,
		Description:      "Maximum local quality; needs a workstation-class GPU",
	},
}

// RecommendModel keeps the original RAM-only API for callers that have no
// detailed hardware profile.
func RecommendModel(ramGB int) *ModelRecommendation {
	return RecommendForSystem(&SystemInfo{
		RAMGB:      ramGB,
		CPUCores:   8,
		CPUThreads: 8,
		DiskFreeGB: 100,
	})
}

// RecommendForSystem chooses the best usable model for the detected machine.
// A discrete GPU is preferred so the model stays on fast VRAM. Without one,
// the largest model that fits RAM, CPU and disk is selected.
func RecommendForSystem(info *SystemInfo) *ModelRecommendation {
	if info == nil {
		return nil
	}

	gpu := bestDiscreteGPU(info)
	best := modelCatalog[0]
	bestDevice := "CPU"

	for _, candidate := range modelCatalog {
		if !hasEnoughDisk(info.DiskFreeGB, candidate.DownloadGB) {
			continue
		}

		if gpu != nil {
			if gpu.VRAMGB >= candidate.VRAMRequiredGB {
				best = candidate
				bestDevice = "GPU"
			}
			continue
		}

		// CPU-only systems should not try to run a 72B model even with huge RAM;
		// it would be technically possible but unusably slow.
		if candidate.DownloadGB > 20 {
			continue
		}
		if info.CPUCores > 0 && info.CPUCores < candidate.CPUCoresRequired {
			continue
		}
		if info.RAMGB < candidate.RAMRequired {
			continue
		}
		best = candidate
		bestDevice = "CPU"
	}

	recommendation := best
	recommendation.Device = bestDevice
	recommendation.PullCommand = fmt.Sprintf("ollama pull %s", recommendation.Model)
	return &recommendation
}

func hasEnoughDisk(freeGB int, downloadGB float64) bool {
	if freeGB <= 0 {
		return true
	}
	// Leave room for the OS, Ollama and a comfortable working margin.
	return float64(freeGB) >= math.Ceil(downloadGB)+5
}

// ModelDownloadGB returns the approximate download size for a catalog model,
// or zero for an unknown tag.
func ModelDownloadGB(name string) float64 {
	name = strings.TrimSuffix(strings.TrimSpace(name), ":latest")
	for _, model := range modelCatalog {
		if strings.TrimSuffix(model.Model, ":latest") == name {
			return model.DownloadGB
		}
	}
	return 0
}

func bestDiscreteGPU(info *SystemInfo) *GPUInfo {
	var best *GPUInfo
	for index := range info.GPUs {
		gpu := &info.GPUs[index]
		if gpu.Integrated || gpu.VRAMGB < 3.5 {
			continue
		}
		if best == nil || gpu.VRAMGB > best.VRAMGB {
			best = gpu
		}
	}
	return best
}
