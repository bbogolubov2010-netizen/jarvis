package commands

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchAppRejectsRelativePath(t *testing.T) {
	result := LaunchApp(InstalledApp{Name: "Relative", Path: "relative.exe"})
	if !strings.Contains(result, "not absolute") {
		t.Fatalf("result = %q, want an absolute-path error", result)
	}
}

func TestLaunchAppRejectsMissingTarget(t *testing.T) {
	result := LaunchApp(InstalledApp{Name: "Missing", Path: filepath.Join(t.TempDir(), "missing.exe")})
	if !strings.Contains(result, "Failed to launch") {
		t.Fatalf("result = %q, want a launch error", result)
	}
}

func TestLaunchAppRejectsDirectory(t *testing.T) {
	result := LaunchApp(InstalledApp{Name: "Folder", Path: t.TempDir()})
	if !strings.Contains(result, "directory") {
		t.Fatalf("result = %q, want a directory error", result)
	}
}
