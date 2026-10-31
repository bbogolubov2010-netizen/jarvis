//go:build manual

package voiceinput

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These tests require the external engine and a WAV file. They are skipped in
// normal test runs and are useful for checking a fresh installation:
//
//	JARVIS_VOICE_ENGINE=build/bin/voice-engine JARVIS_TEST_WAV=test.wav go test ./internal/voiceinput -run Manual -v
func TestManualVoicePipeline(t *testing.T) {
	engine := os.Getenv("JARVIS_VOICE_ENGINE")
	if engine == "" {
		t.Skip("JARVIS_VOICE_ENGINE is not set")
	}
	wav := os.Getenv("JARVIS_TEST_WAV")
	if wav == "" {
		t.Skip("JARVIS_TEST_WAV is not set")
	}
	pcm, err := readWAV16(wav)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("samples=%d duration=%s", len(pcm), time.Duration(len(pcm))*time.Second/16000)

	model, err := LoadVoskModel(filepath.Join(engine, "libvosk.dll"), filepath.Join(engine, "vosk-model-small-ru-0.22"))
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	rec, err := model.NewRecognizer()
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	for start := 0; start < len(pcm); start += 4000 {
		end := start + 4000
		if end > len(pcm) {
			end = len(pcm)
		}
		if _, err := rec.Accept(pcm[start:end]); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("vosk: %q", rec.Final())

	transcriber := NewTranscriber(filepath.Join(engine, "whisper", "whisper-cli.exe"), filepath.Join(engine, "whisper", "ggml-large-v3-turbo.bin"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	text, err := transcriber.Transcribe(ctx, pcm)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("whisper: %q", text)
}

func TestManualSegmenter(t *testing.T) {
	wav := os.Getenv("JARVIS_TEST_WAV")
	if wav == "" {
		t.Skip("JARVIS_TEST_WAV is not set")
	}
	pcm, err := readWAV16(wav)
	if err != nil {
		t.Fatal(err)
	}
	segmenter := NewSegmenter()
	var utterances [][]int16
	for start := 0; start < len(pcm); start += frameSamples {
		end := start + frameSamples
		if end > len(pcm) {
			end = len(pcm)
		}
		for _, utterance := range segmenter.Feed(pcm[start:end]) {
			utterances = append(utterances, utterance)
		}
	}
	for _, utterance := range segmenter.Flush() {
		utterances = append(utterances, utterance)
	}
	t.Logf("utterances=%d", len(utterances))
	for i, utterance := range utterances {
		t.Logf("%d: %s (%d samples)", i, time.Duration(len(utterance))*time.Second/16000, len(utterance))
	}
}

func TestManualCapture(t *testing.T) {
	device := os.Getenv("JARVIS_TEST_DEVICE")
	capture, err := StartCapture(device)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	t.Logf("device=%q", device)
	deadline := time.After(2 * time.Second)
	frames := 0
	var peak float64
	for {
		select {
		case frame, ok := <-capture.Frames():
			if !ok {
				t.Fatal("capture closed")
			}
			frames++
			if level := normalizedRMS(frame); level > peak {
				peak = level
			}
		case <-deadline:
			t.Logf("frames=%d peak=%.4f", frames, peak)
			if frames == 0 {
				t.Fatal("no microphone frames")
			}
			return
		}
	}
}

func TestManualCaptureRecord(t *testing.T) {
	output := os.Getenv("JARVIS_TEST_RECORD")
	if output == "" {
		t.Skip("JARVIS_TEST_RECORD is not set")
	}
	device := os.Getenv("JARVIS_TEST_DEVICE")
	capture, err := StartCapture(device)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()

	segmenter := NewSegmenter()
	var all []int16
	deadline := time.After(7 * time.Second)
	for recording := true; recording; {
		select {
		case frame, ok := <-capture.Frames():
			if !ok {
				recording = false
				break
			}
			all = append(all, frame...)
			segmenter.Feed(frame)
		case <-deadline:
			recording = false
		}
	}
	if err := writePCM16WAV(output, all, 16000); err != nil {
		t.Fatal(err)
	}
	t.Logf("recorded %s to %s", time.Duration(len(all))*time.Second/16000, output)

	engine := os.Getenv("JARVIS_VOICE_ENGINE")
	if engine == "" {
		return
	}
	model, err := LoadVoskModel(filepath.Join(engine, "libvosk.dll"), filepath.Join(engine, "vosk-model-small-ru-0.22"))
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	rec, err := model.NewRecognizer()
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	for start := 0; start < len(all); start += 4000 {
		end := start + 4000
		if end > len(all) {
			end = len(all)
		}
		_, _ = rec.Accept(all[start:end])
	}
	t.Logf("vosk: %q", rec.Final())

	transcriber := NewTranscriber(filepath.Join(engine, "whisper", "whisper-cli.exe"), filepath.Join(engine, "whisper", "ggml-large-v3-turbo.bin"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	text, err := transcriber.Transcribe(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("whisper: %q", text)
}

func TestManualDevices(t *testing.T) {
	devices, err := ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	for _, device := range devices {
		t.Logf("%s default=%v id=%s", device.Name, device.IsDefault, device.ID)
	}
}

func readWAV16(path string) ([]int16, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("not a WAV file")
	}
	offset := 12
	var channels, bits, rate int
	var payload []byte
	for offset+8 <= len(data) {
		id := string(data[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		if offset+size > len(data) {
			break
		}
		chunk := data[offset : offset+size]
		switch id {
		case "fmt ":
			channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
			rate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bits = int(binary.LittleEndian.Uint16(chunk[14:16]))
		case "data":
			payload = chunk
		}
		offset += size
		if size%2 == 1 {
			offset++
		}
	}
	if len(payload) == 0 || bits != 16 || channels == 0 {
		return nil, fmt.Errorf("unsupported WAV format channels=%d bits=%d", channels, bits)
	}
	samples := make([]int16, len(payload)/2/channels)
	for i := 0; i < len(samples); i++ {
		var sum int
		for channel := 0; channel < channels; channel++ {
			index := (i*channels + channel) * 2
			sum += int(int16(binary.LittleEndian.Uint16(payload[index : index+2])))
		}
		samples[i] = int16(sum / channels)
	}
	if rate == 16000 {
		return samples, nil
	}
	return resample(samples, rate, 16000), nil
}

func resample(src []int16, from, to int) []int16 {
	if from == to || len(src) == 0 {
		return src
	}
	outLen := int(float64(len(src)) * float64(to) / float64(from))
	out := make([]int16, outLen)
	ratio := float64(from) / float64(to)
	for i := range out {
		position := float64(i) * ratio
		index := int(position)
		fraction := position - float64(index)
		if index+1 >= len(src) {
			out[i] = src[len(src)-1]
			continue
		}
		out[i] = int16(float64(src[index])*(1-fraction) + float64(src[index+1])*fraction)
	}
	return out
}
