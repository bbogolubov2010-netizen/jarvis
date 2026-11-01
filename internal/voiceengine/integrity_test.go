package voiceengine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, root string, files map[string]string, onDemand map[string]checksumDigest) {
	t.Helper()
	data, err := json.Marshal(checksumManifest{
		Algorithm: "sha256",
		Files:     files,
		OnDemand:  onDemand,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, checksumManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func digestOf(t *testing.T, path string) string {
	t.Helper()
	value, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestVerifyOnDemandAcceptsMatchingModel(t *testing.T) {
	root := t.TempDir()
	core := filepath.Join(root, "libvosk.dll")
	model := filepath.Join(root, "whisper", "ggml-large-v3-turbo.bin")
	writeFile(t, core, "core")
	writeFile(t, model, "model data")

	writeManifest(t, root, map[string]string{
		"libvosk.dll": digestOf(t, core),
	}, map[string]checksumDigest{
		"whisper/ggml-large-v3-turbo.bin": {SHA256: digestOf(t, model), Size: int64(len("model data"))},
	})

	paths := Locate(root)
	if err := paths.Verify(); err != nil {
		t.Fatalf("startup verify: %v", err)
	}
	if err := paths.VerifyOnDemand(); err != nil {
		t.Fatalf("on-demand verify: %v", err)
	}
}

func TestVerifyOnDemandRejectsTamperedModel(t *testing.T) {
	root := t.TempDir()
	core := filepath.Join(root, "libvosk.dll")
	model := filepath.Join(root, "whisper", "ggml-large-v3-turbo.bin")
	writeFile(t, core, "core")
	writeFile(t, model, "model data")

	writeManifest(t, root, map[string]string{
		"libvosk.dll": digestOf(t, core),
	}, map[string]checksumDigest{
		"whisper/ggml-large-v3-turbo.bin": {SHA256: strings.Repeat("0", 64), Size: int64(len("model data"))},
	})

	paths := Locate(root)
	// The fast path must not read the model at all.
	if err := paths.Verify(); err != nil {
		t.Fatalf("startup verify must ignore on-demand files: %v", err)
	}
	if err := paths.VerifyOnDemand(); err == nil {
		t.Fatal("tampered model passed on-demand verification")
	}
}

func TestVerifyOnDemandRejectsSizeMismatch(t *testing.T) {
	root := t.TempDir()
	core := filepath.Join(root, "libvosk.dll")
	model := filepath.Join(root, "whisper", "ggml-large-v3-turbo.bin")
	writeFile(t, core, "core")
	writeFile(t, model, "model data")

	writeManifest(t, root, map[string]string{
		"libvosk.dll": digestOf(t, core),
	}, map[string]checksumDigest{
		"whisper/ggml-large-v3-turbo.bin": {SHA256: digestOf(t, model), Size: 1},
	})

	if err := Locate(root).VerifyOnDemand(); err == nil {
		t.Fatal("size mismatch passed on-demand verification")
	}
}

func TestManifestRejectsEscapingStartupPath(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, map[string]string{
		"../outside.dll": strings.Repeat("0", 64),
	}, nil)
	if err := Locate(root).Verify(); err == nil {
		t.Fatal("escaping startup path was accepted")
	}
}

func TestManifestRejectsEscapingOnDemandPath(t *testing.T) {
	root := t.TempDir()
	core := filepath.Join(root, "libvosk.dll")
	writeFile(t, core, "core")
	writeManifest(t, root, map[string]string{
		"libvosk.dll": digestOf(t, core),
	}, map[string]checksumDigest{
		"../outside.bin": {SHA256: strings.Repeat("0", 64)},
	})

	paths := Locate(root)
	if err := paths.Verify(); err != nil {
		t.Fatalf("startup verify: %v", err)
	}
	if err := paths.VerifyOnDemand(); err == nil {
		t.Fatal("escaping on-demand path was accepted")
	}
}

func TestManifestWithoutStartupFilesIsRejected(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, nil, map[string]checksumDigest{
		"model.bin": {SHA256: strings.Repeat("0", 64)},
	})
	if err := Locate(root).Verify(); err == nil {
		t.Fatal("manifest without startup files was accepted")
	}
}
