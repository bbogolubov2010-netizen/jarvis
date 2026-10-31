//go:build windows

package voiceinput

import (
	"os"
	"testing"
)

func TestDiagnoseVerdictNoDevices(t *testing.T) {
	verdict, hints := diagnoseVerdict(MicrophoneDiagnostics{})
	if verdict == "" || len(hints) == 0 {
		t.Fatalf("expected a no-device verdict, got %q %v", verdict, hints)
	}
}

func TestDiagnoseVerdictPrivacyDenied(t *testing.T) {
	verdict, hints := diagnoseVerdict(MicrophoneDiagnostics{
		DeviceCount:     1,
		PrivacyAllowed:  false,
		ActiveEndpoints: 1,
		Peak:            0.2,
	})
	if verdict == "" || len(hints) == 0 {
		t.Fatalf("expected a privacy verdict, got %q %v", verdict, hints)
	}
}

func TestDiagnoseVerdictDisabledEndpoint(t *testing.T) {
	verdict, hints := diagnoseVerdict(MicrophoneDiagnostics{
		DeviceCount:       1,
		PrivacyAllowed:    true,
		DisabledEndpoints: 1,
	})
	if verdict == "" || len(hints) == 0 {
		t.Fatalf("expected a disabled verdict, got %q %v", verdict, hints)
	}
}

func TestDiagnoseVerdictSignalLevels(t *testing.T) {
	base := MicrophoneDiagnostics{DeviceCount: 1, PrivacyAllowed: true, ActiveEndpoints: 1}
	base.Peak = 0.0005
	if verdict, _ := diagnoseVerdict(base); verdict == "" {
		t.Fatal("expected a silent verdict")
	}
	base.Peak = 0.005
	if verdict, _ := diagnoseVerdict(base); verdict == "" {
		t.Fatal("expected a low-level verdict")
	}
	base.Peak = 0.2
	if verdict, hints := diagnoseVerdict(base); verdict == "" || len(hints) != 0 {
		t.Fatalf("expected a working verdict without hints, got %q %v", verdict, hints)
	}
}

func TestMicrophoneDiagnosticsLive(t *testing.T) {
	if os.Getenv("JARVIS_MICTEST") != "1" {
		t.Skip("set JARVIS_MICTEST=1 to measure the real microphone")
	}
	diag := DiagnoseMicrophone("")
	t.Log(FormatDiagnostics(diag))
	if diag.Error != "" {
		t.Fatalf("diagnostics error: %s", diag.Error)
	}
	if diag.Verdict == "" {
		t.Fatal("empty verdict")
	}
}
