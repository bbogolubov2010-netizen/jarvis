package main

import (
	"testing"

	"Jarvis/internal/config"
)

func TestStripConfiguredWake(t *testing.T) {
	app := &App{settings: config.Settings{VoiceWakeWord: "компьютер"}}

	rest, ok := app.stripConfiguredWake("Компьютер, какая погода?")
	if !ok || rest != "какая погода" {
		t.Fatalf("custom wake: rest=%q ok=%v", rest, ok)
	}
	rest, ok = app.stripConfiguredWake("компьютер")
	if !ok || rest != "" {
		t.Fatalf("wake-only: rest=%q ok=%v", rest, ok)
	}
	if _, ok := app.stripConfiguredWake("Привет, Джарвис, который час"); ok {
		t.Fatal("default wake should be handled by intent.StripWakeWord")
	}
}
