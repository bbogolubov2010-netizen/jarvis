// Package voiceengine locates the external speech engines and models. Large
// binaries and models deliberately live outside the executable.
package voiceengine

import (
	"os"
	"path/filepath"
)

// Paths contains every external file used by the voice pipeline.
type Paths struct {
	Root         string
	VoskDLL      string
	VoskModel    string
	VoskModelEn  string
	WhisperCLI   string
	WhisperModel string
}

// Status describes which parts of the engine are installed.
type Status struct {
	Root           string `json:"root"`
	VoskReady      bool   `json:"vosk_ready"`
	VoskReadyEn    bool   `json:"vosk_ready_en"`
	WhisperReady   bool   `json:"whisper_ready"`
	WhisperModel   string `json:"whisper_model"`
	WhisperBackend string `json:"whisper_backend"`
	IntegrityError string `json:"integrity_error"`
}

// DefaultRoot returns the writable per-user engine directory.
func DefaultRoot() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "Jarvis", "voice-engine")
}

// Resolve chooses the first engine directory that contains a usable Vosk
// installation. The folder next to the executable wins so a portable build can
// ship its engines without copying gigabytes into AppData.
func Resolve() Paths {
	candidates := make([]string, 0, 5)
	if executable, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(exeDir, "voice-engine"),
			filepath.Join(exeDir, "..", "voice-engine"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "voice-engine"),
			filepath.Join(cwd, "build", "bin", "voice-engine"),
		)
	}
	candidates = append(candidates, DefaultRoot())

	var partial *Paths
	for _, root := range candidates {
		p := Locate(root)
		if p.Ready() {
			return p
		}
		if partial == nil && (p.VoskReady() || p.VoskReadyEn() || p.WhisperReady()) {
			copied := p
			partial = &copied
		}
	}
	if partial != nil {
		return *partial
	}
	return Locate(DefaultRoot())
}

// Locate builds paths for a specific engine root without checking existence.
func Locate(root string) Paths {
	root = filepath.Clean(root)
	whisperDir := filepath.Join(root, "whisper")
	p := Paths{
		Root:        root,
		VoskDLL:     filepath.Join(root, "libvosk.dll"),
		VoskModel:   filepath.Join(root, "vosk-model-small-ru-0.22"),
		VoskModelEn: filepath.Join(root, "vosk-model-small-en-us-0.15"),
		WhisperCLI:  filepath.Join(whisperDir, "whisper-cli.exe"),
	}
	if !exists(p.WhisperCLI) {
		if alt := filepath.Join(root, "whisper-cli.exe"); exists(alt) {
			p.WhisperCLI = alt
		}
	}
	for _, name := range []string{
		"ggml-large-v3-turbo.bin",
		"ggml-large-v3-turbo-q8_0.bin",
		"ggml-large-v3-turbo-q5_0.bin",
		"ggml-large-v3.bin",
		"ggml-large-v3-q5_0.bin",
		"ggml-medium.bin",
		"ggml-small.bin",
	} {
		candidate := filepath.Join(filepath.Dir(p.WhisperCLI), name)
		if exists(candidate) {
			p.WhisperModel = candidate
			break
		}
		candidate = filepath.Join(root, name)
		if exists(candidate) {
			p.WhisperModel = candidate
			break
		}
	}
	if p.WhisperModel == "" {
		p.WhisperModel = filepath.Join(filepath.Dir(p.WhisperCLI), "ggml-large-v3-turbo.bin")
	}
	return p
}

// VoskReady reports whether the Russian wake-word engine can be loaded.
func (p Paths) VoskReady() bool {
	return exists(p.VoskDLL) && dirExists(p.VoskModel)
}

// VoskReadyEn reports whether the optional English wake-word model is present.
func (p Paths) VoskReadyEn() bool {
	return exists(p.VoskDLL) && dirExists(p.VoskModelEn)
}

// WhisperReady reports whether the command recognizer can run.
func (p Paths) WhisperReady() bool {
	return exists(p.WhisperCLI) && exists(p.WhisperModel)
}

// Ready reports whether the full local pipeline is installed.
func (p Paths) Ready() bool {
	return (p.VoskReady() || p.VoskReadyEn()) && p.WhisperReady()
}

// Status returns a serializable summary for the UI.
func (p Paths) Status() Status {
	model := filepath.Base(p.WhisperModel)
	if p.WhisperModel == "" {
		model = ""
	}
	backend := "cpu"
	if filepath.Base(p.WhisperCLI) == "whisper-cli.exe" {
		if _, err := os.Stat(filepath.Join(filepath.Dir(p.WhisperCLI), "cublas64_12.dll")); err == nil {
			backend = "cuda"
		}
	}
	return Status{
		Root:           p.Root,
		VoskReady:      p.VoskReady(),
		VoskReadyEn:    p.VoskReadyEn(),
		WhisperReady:   p.WhisperReady(),
		WhisperModel:   model,
		WhisperBackend: backend,
	}
}

func exists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
