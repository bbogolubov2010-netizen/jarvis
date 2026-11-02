package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// testTempDir removes its directory with retries. Windows can briefly keep a
// freshly written settings file open (antivirus, indexer), which makes
// testing.TempDir cleanup fail intermittently under the race detector.
func testTempDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for {
			err := os.RemoveAll(dir)
			if err == nil || os.IsNotExist(err) {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("remove temp dir %s: %v", dir, err)
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
	})
	return dir
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(testTempDir(t), "settings.json")
	original := DefaultSettings()
	original.Model = "test-model:1b"
	original.WebSearch = false
	original.VoiceDisabled = true

	if err := Save(path, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || strings.Contains(string(data), "test-model:1b") {
		t.Fatal("settings file is not encrypted")
	}

	loaded, found, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("Load did not report an existing file")
	}
	if !reflect.DeepEqual(loaded, original) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", loaded, original)
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	settings, found, err := Load(filepath.Join(testTempDir(t), "missing.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Fatal("found must be false for a missing file")
	}
	if !reflect.DeepEqual(settings, DefaultSettings()) {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
}

func TestNormalizeFillsEmptyValues(t *testing.T) {
	defaults := DefaultSettings()
	got := Normalize(Settings{})

	if got.OllamaURL != defaults.OllamaURL {
		t.Fatalf("OllamaURL = %q", got.OllamaURL)
	}
	if got.Model != defaults.Model {
		t.Fatalf("Model = %q", got.Model)
	}
	if got.SystemPrompt != defaults.SystemPrompt {
		t.Fatal("SystemPrompt was not filled")
	}
	if got.WebSearch {
		t.Fatal("Normalize must not override an explicit false WebSearch value")
	}
}

func TestNormalizeMigratesUntouchedLegacyPrompt(t *testing.T) {
	settings := DefaultSettings()
	settings.SystemPrompt = legacyDefaultPromptV1
	settings.SystemPromptVersion = 0
	settings.SystemPromptCustomized = false

	got := Normalize(settings)
	if got.SystemPrompt != DefaultSystemPrompt() {
		t.Fatal("legacy prompt was not upgraded")
	}
	if got.SystemPromptVersion != currentSystemPromptVersion {
		t.Fatalf("version = %d, want %d", got.SystemPromptVersion, currentSystemPromptVersion)
	}
	if got.SystemPromptCustomized {
		t.Fatal("an untouched legacy prompt must not be marked customized")
	}
}

func TestNormalizeKeepsCustomPrompt(t *testing.T) {
	settings := DefaultSettings()
	settings.SystemPrompt = "You are my custom butler."
	settings.SystemPromptVersion = 0

	got := Normalize(settings)
	if got.SystemPrompt != "You are my custom butler." {
		t.Fatalf("custom prompt was overwritten: %q", got.SystemPrompt)
	}
	if !got.SystemPromptCustomized {
		t.Fatal("custom prompt must be marked customized")
	}
}

func TestNormalizeAutonomyLevel(t *testing.T) {
	cases := map[string]string{
		"":             "suggestions",
		"OFF":          "off",
		" Trusted ":    "trusted",
		"unrestricted": "suggestions",
	}
	for input, want := range cases {
		settings := DefaultSettings()
		settings.AutonomyLevel = input
		if got := Normalize(settings).AutonomyLevel; got != want {
			t.Errorf("Normalize(AutonomyLevel=%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeVoiceLanguage(t *testing.T) {
	cases := map[string]string{
		"":      "auto",
		"AUTO":  "auto",
		" RU ":  "ru",
		"en":    "en",
		"de":    "auto",
		"kling": "auto",
	}
	for input, want := range cases {
		settings := DefaultSettings()
		settings.VoiceLanguage = input
		if got := Normalize(settings).VoiceLanguage; got != want {
			t.Errorf("Normalize(VoiceLanguage=%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLoadDetailedReportsPlaintextAndEncryptedFiles(t *testing.T) {
	dir := testTempDir(t)
	path := filepath.Join(dir, "settings.json")

	encoded, err := json.Marshal(DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, plaintext, err := LoadDetailed(path); err != nil || !found || !plaintext {
		t.Fatalf("plaintext LoadDetailed = found:%v plaintext:%v err:%v", found, plaintext, err)
	}

	if err := Save(path, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if _, found, plaintext, err := LoadDetailed(path); err != nil || !found || plaintext {
		t.Fatalf("encrypted LoadDetailed = found:%v plaintext:%v err:%v", found, plaintext, err)
	}
}

func TestLoadPlaintextJSONWithLeadingWhitespace(t *testing.T) {
	path := filepath.Join(testTempDir(t), "settings.json")
	expected := DefaultSettings()
	encoded, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append([]byte(" \r\n\t"), encoded...), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, found, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("Load did not report an existing file")
	}
	if !reflect.DeepEqual(loaded, expected) {
		t.Fatalf("unexpected settings: %+v", loaded)
	}
}

func TestLoadPlaintextJSONWithBOM(t *testing.T) {
	path := filepath.Join(testTempDir(t), "settings.json")
	expected := DefaultSettings()
	encoded, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, encoded...)
	if err := os.WriteFile(path, withBOM, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, found, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("Load did not report an existing file")
	}
	if !reflect.DeepEqual(loaded, expected) {
		t.Fatalf("unexpected settings: %+v", loaded)
	}
}

func TestSaveLeavesNoTemporaryFile(t *testing.T) {
	dir := testTempDir(t)
	path := filepath.Join(dir, "settings.json")
	if err := Save(path, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".settings.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}
}

func TestLoadLegacySettingsWithoutVoiceFieldKeepsVoiceEnabled(t *testing.T) {
	path := filepath.Join(testTempDir(t), "settings.json")
	legacy := `{"ollama_url":"http://localhost:11434","model":"llama3.1:8b","system_prompt":"legacy","web_search":true}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, found, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("Load did not report an existing file")
	}
	if loaded.VoiceDisabled {
		t.Fatal("legacy settings without voice_disabled must keep voice enabled")
	}
}

func TestBackupCorruptPreservesOriginalBytes(t *testing.T) {
	dir := testTempDir(t)
	path := filepath.Join(dir, "settings.json")
	original := []byte("{corrupt settings that must be recoverable")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := BackupCorrupt(path)
	if err != nil {
		t.Fatalf("BackupCorrupt: %v", err)
	}
	if backup == "" {
		t.Fatal("no backup path returned")
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("backup content = %q, want %q", data, original)
	}
}

func TestLoadCorruptFileReturnsDefaultsAndError(t *testing.T) {
	path := filepath.Join(testTempDir(t), "settings.json")
	if err := os.WriteFile(path, []byte("{not encrypted and not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, found, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for corrupt settings")
	}
	if !found {
		t.Fatal("found must be true for a corrupt existing file")
	}
	if !reflect.DeepEqual(settings, DefaultSettings()) {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
}
