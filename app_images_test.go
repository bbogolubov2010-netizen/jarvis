package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeCapture(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-age)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPruneImageDirRemovesStaleFiles(t *testing.T) {
	dir := t.TempDir()
	stale := writeCapture(t, dir, "old.png", 2*time.Hour)
	fresh := writeCapture(t, dir, "new.png", time.Minute)

	pruneImageDir(dir)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale capture was not removed: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh capture was removed: %v", err)
	}
}

func TestPruneImageDirCapsFileCount(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < maxRetainedImages+5; i++ {
		writeCapture(t, dir, fmt.Sprintf("shot_%02d.png", i), time.Duration(i)*time.Minute)
	}

	pruneImageDir(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxRetainedImages {
		t.Fatalf("kept %d files, want %d", len(entries), maxRetainedImages)
	}
}

func TestPruneImageDirIgnoresMissingDirectory(t *testing.T) {
	pruneImageDir(filepath.Join(t.TempDir(), "missing"))
}

func TestScreenConsentIsOneShotAndExpires(t *testing.T) {
	app := &App{}
	if app.consumeScreenConsent() {
		t.Fatal("consent was accepted without being granted")
	}
	if !app.grantScreenConsent(time.Minute) {
		t.Fatal("grantScreenConsent returned false")
	}
	if !app.consumeScreenConsent() {
		t.Fatal("granted consent was not accepted")
	}
	if app.consumeScreenConsent() {
		t.Fatal("consent was accepted twice")
	}

	app.screenConsentUntil = time.Now().Add(-time.Second)
	if app.consumeScreenConsent() {
		t.Fatal("expired consent was accepted")
	}
}

func TestProcessWithScreenRequiresConsent(t *testing.T) {
	app := &App{}
	got := app.ProcessWithScreen("what is on screen")
	if !strings.Contains(got, "confirmation") {
		t.Fatalf("ProcessWithScreen without consent = %q", got)
	}
}

func TestWaitForStreamsTimesOut(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()

	start := time.Now()
	if waitForStreams(&wg, 20*time.Millisecond) {
		t.Fatal("waitForStreams reported completion while the group was busy")
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("waitForStreams returned too early: %v", elapsed)
	}
}
