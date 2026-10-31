package commands

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalAppNameRussianAliases(t *testing.T) {
	tests := map[string]string{
		"проводник":        "explorer",
		"блокнот":          "notepad",
		"калькулятор":      "calc",
		"командная строка": "cmd",
		"настройки":        "settings",
		"хром":             "chrome",
		"телеграм":         "telegram",
		"неизвестное":      "неизвестное",
	}
	for input, want := range tests {
		if got := canonicalAppName(input); got != want {
			t.Errorf("canonicalAppName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuiltInExplorerUsesSystemRoot(t *testing.T) {
	path := builtInApps["explorer"].path
	if !filepath.IsAbs(path) {
		t.Fatalf("explorer path is not absolute: %q", path)
	}
	if strings.EqualFold(filepath.Base(path), "explorer.exe") == false {
		t.Fatalf("explorer path has the wrong filename: %q", path)
	}
	if strings.Contains(strings.ToLower(path), `system32`) {
		t.Fatalf("explorer must not resolve inside System32: %q", path)
	}
}

func TestSearchWebContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := SearchWebContext(ctx, "test"); !strings.Contains(got, "cancelled") {
		t.Fatalf("canceled search returned %q", got)
	}
}

func TestGenerateImageContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := GenerateImageContext(ctx, "cat", "512x512"); !strings.Contains(got, "cancelled") {
		t.Fatalf("canceled image request returned %q", got)
	}
}
