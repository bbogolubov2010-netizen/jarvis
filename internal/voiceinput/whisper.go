package voiceinput

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unicode"
)

// Transcriber runs whisper.cpp on a completed utterance. It deliberately uses
// the official CLI instead of linking a second C++ runtime into the app.
type Transcriber struct {
	CLIPath string
	Model   string
	Threads int
	Prompt  string
	// Language is "auto", "ru", or "en". Empty is treated as "auto" so the
	// model detects Russian and English without a settings change.
	Language string
}

// NewTranscriber creates a bilingual whisper.cpp transcriber.
func NewTranscriber(cliPath, modelPath string) *Transcriber {
	threads := runtime.NumCPU() / 2
	if threads < 4 {
		threads = 4
	}
	if threads > 12 {
		threads = 12
	}
	return &Transcriber{
		CLIPath:  cliPath,
		Model:    modelPath,
		Threads:  threads,
		Language: "auto",
		Prompt: "Jarvis voice commands: weather, open browser, search the web, volume, screenshot. " +
			"Команды ассистенту Джарвис: погода, открой браузер, найди в интернете, громкость, скриншот.",
	}
}

func (t *Transcriber) language() string {
	switch strings.ToLower(strings.TrimSpace(t.Language)) {
	case "ru", "en":
		return strings.ToLower(strings.TrimSpace(t.Language))
	default:
		return "auto"
	}
}

// Available reports whether the CLI and model are present.
func (t *Transcriber) Available() bool {
	if t == nil || t.CLIPath == "" || t.Model == "" {
		return false
	}
	if _, err := os.Stat(t.CLIPath); err != nil {
		return false
	}
	if _, err := os.Stat(t.Model); err != nil {
		return false
	}
	return true
}

// Transcribe writes PCM16 mono 16 kHz audio to a temporary WAV and asks
// whisper.cpp for a Russian transcript.
func (t *Transcriber) Transcribe(ctx context.Context, pcm []int16) (string, error) {
	if !t.Available() {
		return "", fmt.Errorf("Whisper engine is not installed")
	}
	if len(pcm) < 1600 { // less than 100 ms
		return "", nil
	}

	dir, err := os.MkdirTemp("", "jarvis-whisper-*")
	if err != nil {
		return "", fmt.Errorf("create whisper temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	wavPath := filepath.Join(dir, "command.wav")
	if err := writePCM16WAV(wavPath, pcm, 16000); err != nil {
		return "", err
	}
	prefix := filepath.Join(dir, "result")
	args := []string{
		"-m", t.Model,
		"-f", wavPath,
		"-l", t.language(),
		"-nt",
		"-otxt",
		"-of", prefix,
		"-t", fmt.Sprintf("%d", t.Threads),
		"-bs", "5",
		"-bo", "5",
		"-np",
	}
	if t.Prompt != "" {
		args = append(args, "--prompt", t.Prompt)
	}

	cmd := exec.CommandContext(ctx, t.CLIPath, args...)
	cmd.Dir = filepath.Dir(t.CLIPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = withPath(os.Environ(), filepath.Dir(t.CLIPath))
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("whisper: %w: %s", err, strings.TrimSpace(string(output)))
	}

	data, err := os.ReadFile(prefix + ".txt")
	if err != nil {
		return "", fmt.Errorf("read whisper result: %w", err)
	}
	return cleanTranscript(string(data)), nil
}

func cleanTranscript(text string) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "«»\"“”'`")
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	if lower == "" || lower == "[blank_audio]" || lower == "(silence)" || lower == "[silence]" {
		return ""
	}
	if strings.HasPrefix(lower, "[") && strings.HasSuffix(lower, "]") {
		return ""
	}
	// Whisper sometimes invents video-subtitle phrases on noise or silence.
	// These are never useful commands, so drop them before they reach intent
	// parsing. Punctuation is flattened to spaces so that "Amara.org" and
	// "Amara org" compare the same.
	normalized := noiseKey(text)
	if normalized == "" {
		return ""
	}
	// Substring markers are distinctive enough that they never appear in a
	// real command.
	for _, marker := range []string{
		"субтитр",
		"продолжение следует",
		"спасибо за просмотр",
		"подписывайтесь на канал",
		"ставьте лайки",
		"не забудьте подписаться",
		"thank you for watching",
		"thanks for watching",
		"please subscribe",
		"like and subscribe",
		"subscribe to my channel",
		"see you next time",
		"subtitled by",
		"transcribed by",
		"amara org",
	} {
		if strings.Contains(normalized, marker) {
			return ""
		}
	}
	// Short single-word hallucinations only match the whole utterance.
	for _, phrase := range []string{
		"корректор",
		"до встречи",
		"всем пока",
		"музыка",
		"аплодисменты",
		"смех",
		"тишина",
		"music",
		"applause",
		"laughter",
		"silence",
		"subtitles",
		"you",
	} {
		if normalized == phrase {
			return ""
		}
	}
	return text
}

// noiseKey lowercases text and reduces every non-alphanumeric rune to a single
// space so punctuation variants of the same phrase compare equal.
func noiseKey(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	space := false
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func writePCM16WAV(path string, pcm []int16, sampleRate int) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create wav: %w", err)
	}
	defer file.Close()

	dataSize := uint32(len(pcm) * 2)
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataSize)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:24], 1) // mono
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataSize)
	if _, err := file.Write(header); err != nil {
		return fmt.Errorf("write wav header: %w", err)
	}

	buf := make([]byte, len(pcm)*2)
	for i, sample := range pcm {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(sample))
	}
	if _, err := file.Write(buf); err != nil {
		return fmt.Errorf("write wav data: %w", err)
	}
	return nil
}

func withPath(env []string, dir string) []string {
	for i, item := range env {
		if strings.HasPrefix(strings.ToUpper(item), "PATH=") {
			env[i] = "PATH=" + dir + ";" + item[len("PATH="):]
			return env
		}
	}
	return append(env, "PATH="+dir)
}
