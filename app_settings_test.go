package main

import (
	"testing"

	"Jarvis/internal/config"
)

func TestVoiceSettingsChanged(t *testing.T) {
	base := config.DefaultSettings()

	if voiceSettingsChanged(base, base) {
		t.Fatal("identical settings must not restart voice input")
	}

	cases := map[string]func(*config.Settings){
		"voice disabled":         func(s *config.Settings) { s.VoiceDisabled = !s.VoiceDisabled },
		"voice input disabled":   func(s *config.Settings) { s.VoiceInputDisabled = !s.VoiceInputDisabled },
		"wake word disabled":     func(s *config.Settings) { s.WakeWordDisabled = !s.WakeWordDisabled },
		"dangerous confirmation": func(s *config.Settings) { s.VoiceConfirmDangerousDisabled = !s.VoiceConfirmDangerousDisabled },
		"voice agent disabled":   func(s *config.Settings) { s.VoiceAgentDisabled = !s.VoiceAgentDisabled },
		"wake word":              func(s *config.Settings) { s.VoiceWakeWord = s.VoiceWakeWord + " test" },
		"language":               func(s *config.Settings) { s.VoiceLanguage = "en" },
		"device":                 func(s *config.Settings) { s.VoiceDeviceID = "device-2" },
		"device name":            func(s *config.Settings) { s.VoiceDeviceName = "Headset" },
		"whisper model":          func(s *config.Settings) { s.VoiceWhisperModel = "medium" },
	}
	for name, mutate := range cases {
		next := base
		mutate(&next)
		if !voiceSettingsChanged(base, next) {
			t.Errorf("%s change was not detected", name)
		}
	}

	// Weather location, model, prompt, autonomy, and search are read on demand.
	// Touching them must not interrupt the microphone.
	unrelated := base
	unrelated.Model = "another-model"
	unrelated.SystemPrompt = "another prompt"
	unrelated.WebSearch = !unrelated.WebSearch
	unrelated.VoiceCity = "Moscow"
	unrelated.VoiceLatitude = 55.75
	unrelated.VoiceLongitude = 37.62
	unrelated.AutonomyLevel = "trusted"
	unrelated.AutoStart = true
	if voiceSettingsChanged(base, unrelated) {
		t.Fatal("unrelated settings must not restart voice input")
	}
}
