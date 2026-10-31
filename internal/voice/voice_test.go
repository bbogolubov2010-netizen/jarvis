package voice

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestPhraseTableIsComplete(t *testing.T) {
	if len(Keys()) == 0 {
		t.Fatal("phrase table is empty")
	}
	for _, key := range Keys() {
		if len(FilesFor(key)) == 0 {
			t.Fatalf("key %q has no files", key)
		}
	}
}

func TestVoiceAssetsExistAndArePCM(t *testing.T) {
	root := filepath.Join("..", "..", "voice")
	if _, err := os.Stat(filepath.Join(root, "greeting.wav")); os.IsNotExist(err) {
		t.Skip("local voice pack is not checked in")
	}
	for _, name := range AllFiles() {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
			t.Fatalf("%s is not a RIFF/WAVE file", name)
		}
		if string(data[12:16]) != "fmt " {
			t.Fatalf("%s has no fmt chunk", name)
		}
		format := binary.LittleEndian.Uint16(data[20:22])
		if format != 1 {
			t.Fatalf("%s uses audio format %d, want PCM", name, format)
		}
		channels := binary.LittleEndian.Uint16(data[22:24])
		rate := binary.LittleEndian.Uint32(data[24:28])
		bits := binary.LittleEndian.Uint16(data[34:36])
		if channels == 0 || rate == 0 || bits == 0 {
			t.Fatalf("%s has an invalid fmt chunk", name)
		}
	}
}

func writeFakePack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range AllFiles() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fake wav"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestEnsureAssetsCopiesMissingFiles(t *testing.T) {
	source := writeFakePack(t)
	dest := t.TempDir()

	if err := EnsureAssets(dest, []string{source}); err != nil {
		t.Fatalf("EnsureAssets: %v", err)
	}
	if missing := missingIn(dest); len(missing) != 0 {
		t.Fatalf("missing after copy: %v", missing)
	}

	// A second run must not fail or overwrite existing files.
	if err := EnsureAssets(dest, []string{source}); err != nil {
		t.Fatalf("second EnsureAssets: %v", err)
	}
}

func TestEnsureAssetsPreservesUserReplacement(t *testing.T) {
	source := writeFakePack(t)
	dest := t.TempDir()
	if err := EnsureAssets(dest, []string{source}); err != nil {
		t.Fatalf("EnsureAssets: %v", err)
	}

	replacement := []byte("user replacement")
	path := filepath.Join(dest, "yes.wav")
	if err := os.WriteFile(path, replacement, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAssets(dest, []string{source}); err != nil {
		t.Fatalf("second EnsureAssets: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(replacement) {
		t.Fatal("EnsureAssets overwrote a user-replaced phrase")
	}
}

func TestUnknownKeyHasNoFiles(t *testing.T) {
	if files := FilesFor(Key("not-a-real-key")); len(files) != 0 {
		t.Fatalf("unknown key returned files: %v", files)
	}
}

func TestPlayerResolveFindsVariant(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "yes.wav"), []byte("not a real wav"), 0o600); err != nil {
		t.Fatal(err)
	}

	player := NewPlayer([]string{dir}, nil)
	if got := player.resolve(KeyAck); got == "" {
		t.Fatal("resolve did not find a variant")
	}
	if !player.Available(KeyAck) {
		t.Fatal("Available reported false")
	}
	if player.Available(KeyGreeting) {
		t.Fatal("Available reported a missing phrase")
	}
}

func TestDisabledPlayerDoesNotPlay(t *testing.T) {
	player := NewPlayer(nil, func() bool { return false })
	if player.Play(KeyAck) {
		t.Fatal("disabled player played a phrase")
	}
}
