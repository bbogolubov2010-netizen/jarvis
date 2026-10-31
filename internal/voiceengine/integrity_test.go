package voiceengine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, root string, files map[string]string) {
	t.Helper()

	data, err := json.Marshal(checksumManifest{Algorithm: "sha256", Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, checksumManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyAcceptsMatchingChecksum(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "engine.exe")
	if err := os.WriteFile(path, []byte("trusted engine"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	writeManifest(t, root, map[string]string{"engine.exe": digest})

	if err := Locate(root).Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerifyRejectsTamperedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "engine.exe")
	if err := os.WriteFile(path, []byte("trusted engine"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	writeManifest(t, root, map[string]string{"engine.exe": digest})
	if err := os.WriteFile(path, []byte("tampered engine"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Locate(root).Verify(); err == nil {
		t.Fatal("tampered engine passed verification")
	}
}

func TestVerifyRejectsUnsafeManifestPath(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, map[string]string{"../outside.dll": "00"})

	if err := Locate(root).Verify(); err == nil {
		t.Fatal("unsafe checksum path was accepted")
	}
}

func TestVerifyAllowsMissingManifestByDefault(t *testing.T) {
	if err := Locate(t.TempDir()).Verify(); err != nil {
		t.Fatalf("missing manifest should be optional: %v", err)
	}
}

func TestVerifyRequiresManifestWhenRequested(t *testing.T) {
	t.Setenv("JARVIS_VOICE_REQUIRE_CHECKSUMS", "1")
	if err := Locate(t.TempDir()).Verify(); err == nil {
		t.Fatal("missing manifest was accepted in required mode")
	}
}
