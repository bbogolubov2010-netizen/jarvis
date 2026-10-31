package setup

import "testing"

func TestRecommendModel(t *testing.T) {
	cases := []struct {
		ram  int
		want string
	}{
		{0, "llama3.2:1b"},
		{4, "llama3.2:1b"},
		{6, "llama3.2:3b"},
		{8, "llama3.2:3b"},
		{10, "llama3.1:8b"},
		{16, "llama3.1:8b"},
		{18, "qwen2.5:14b"},
		{32, "qwen2.5:14b"},
		{36, "qwen2.5:32b"},
		{64, "qwen2.5:32b"},
		{128, "qwen2.5:32b"},
	}

	for _, testCase := range cases {
		recommendation := RecommendModel(testCase.ram)
		if recommendation == nil {
			t.Fatalf("RecommendModel(%d) returned nil", testCase.ram)
		}
		if recommendation.Model != testCase.want {
			t.Fatalf("RecommendModel(%d) = %q, want %q", testCase.ram, recommendation.Model, testCase.want)
		}
		if recommendation.Device != "CPU" {
			t.Fatalf("RecommendModel(%d) device = %q, want CPU", testCase.ram, recommendation.Device)
		}
	}
}

func TestRecommendForSystemUsesDiscreteGPU(t *testing.T) {
	cases := []struct {
		vram float64
		want string
	}{
		{8, "llama3.1:8b"},
		{12, "qwen2.5:14b"},
		{24, "qwen2.5:32b"},
		{48, "qwen2.5:72b"},
	}

	for _, testCase := range cases {
		info := &SystemInfo{
			RAMGB:      64,
			CPUCores:   8,
			DiskFreeGB: 200,
			GPUs: []GPUInfo{{
				Name:   "Test GPU",
				Vendor: "NVIDIA",
				VRAMGB: testCase.vram,
			}},
		}
		recommendation := RecommendForSystem(info)
		if recommendation == nil {
			t.Fatalf("RecommendForSystem(%v GB VRAM) returned nil", testCase.vram)
		}
		if recommendation.Model != testCase.want {
			t.Fatalf("RecommendForSystem(%v GB VRAM) = %q, want %q", testCase.vram, recommendation.Model, testCase.want)
		}
		if recommendation.Device != "GPU" {
			t.Fatalf("RecommendForSystem(%v GB VRAM) device = %q, want GPU", testCase.vram, recommendation.Device)
		}
	}
}

func TestRecommendForSystemRespectsCPUAndDisk(t *testing.T) {
	cases := []struct {
		name string
		info *SystemInfo
		want string
	}{
		{
			name: "few CPU cores cap the model",
			info: &SystemInfo{RAMGB: 64, CPUCores: 4, DiskFreeGB: 200},
			want: "llama3.1:8b",
		},
		{
			name: "small disk chooses smaller model",
			info: &SystemInfo{RAMGB: 64, CPUCores: 16, DiskFreeGB: 8},
			want: "llama3.2:3b",
		},
		{
			name: "integrated GPU uses RAM path",
			info: &SystemInfo{
				RAMGB:      16,
				CPUCores:   8,
				DiskFreeGB: 200,
				GPUs: []GPUInfo{{
					Name:       "Intel UHD",
					Vendor:     "Intel",
					VRAMGB:     0,
					Integrated: true,
				}},
			},
			want: "llama3.1:8b",
		},
	}

	for _, testCase := range cases {
		recommendation := RecommendForSystem(testCase.info)
		if recommendation == nil {
			t.Fatalf("%s: returned nil", testCase.name)
		}
		if recommendation.Model != testCase.want {
			t.Fatalf("%s: got %q, want %q", testCase.name, recommendation.Model, testCase.want)
		}
	}
}

func TestRecommendForSystemNil(t *testing.T) {
	if recommendation := RecommendForSystem(nil); recommendation != nil {
		t.Fatalf("RecommendForSystem(nil) = %+v, want nil", recommendation)
	}
}

func TestModelDownloadGB(t *testing.T) {
	if got := ModelDownloadGB("llama3.1:8b"); got != 4.9 {
		t.Fatalf("ModelDownloadGB(llama3.1:8b) = %v, want 4.9", got)
	}
	if got := ModelDownloadGB("llama3.1:8b:latest"); got != 4.9 {
		t.Fatalf("ModelDownloadGB(:latest) = %v, want 4.9", got)
	}
	if got := ModelDownloadGB("custom:1b"); got != 0 {
		t.Fatalf("ModelDownloadGB(custom) = %v, want 0", got)
	}
}

func TestHasEnoughDisk(t *testing.T) {
	if !hasEnoughDisk(0, 100) {
		t.Fatal("unknown disk size should not block a recommendation")
	}
	if hasEnoughDisk(9, 4.9) {
		t.Fatal("9 GB is not enough for a 4.9 GB model plus margin")
	}
	if !hasEnoughDisk(10, 2.0) {
		t.Fatal("10 GB should fit a 2 GB model")
	}
}
