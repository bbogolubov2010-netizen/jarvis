package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Jarvis/internal/voiceengine"
)

func writeEngineManifest(t *testing.T, root string, files map[string]string) {
	t.Helper()

	data, err := json.Marshal(map[string]any{"algorithm": "sha256", "files": files})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "checksums.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func engineDigest(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestVerifyVoiceEngineRefreshesWhenRootChanges(t *testing.T) {
	app := NewApp(false)

	good := t.TempDir()
	goodEngine := filepath.Join(good, "engine.dll")
	if err := os.WriteFile(goodEngine, []byte("trusted engine"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeEngineManifest(t, good, map[string]string{"engine.dll": engineDigest(t, goodEngine)})
	if err := app.verifyVoiceEngine(voiceengine.Locate(good)); err != nil {
		t.Fatalf("verify good root: %v", err)
	}

	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "engine.dll"), []byte("tampered engine"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeEngineManifest(t, bad, map[string]string{"engine.dll": strings.Repeat("0", 64)})
	if err := app.verifyVoiceEngine(voiceengine.Locate(bad)); err == nil {
		t.Fatal("tampered root passed verification")
	}

	// Re-checking the same failing root must not clear the cached error, so a
	// status refresh cannot silently re-enable a compromised engine.
	if err := app.verifyVoiceEngine(voiceengine.Locate(bad)); err == nil {
		t.Fatal("cached integrity error was cleared without a root change")
	}

	// A repaired engine in a different root is picked up without a restart.
	if err := app.verifyVoiceEngine(voiceengine.Locate(good)); err != nil {
		t.Fatalf("verify repaired root: %v", err)
	}
}
