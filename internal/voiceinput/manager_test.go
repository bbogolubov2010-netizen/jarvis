package voiceinput

import (
	"testing"

	"Jarvis/internal/voiceengine"
)

func TestManagerWakeText(t *testing.T) {
	manager := NewManager(voiceengine.Paths{}, "", Callbacks{})
	manager.SetWakeWord("компьютер")

	rest, ok := manager.wakeText("Компьютер, какая погода?")
	if !ok || rest != "какая погода" {
		t.Fatalf("custom wake: rest=%q ok=%v", rest, ok)
	}
	rest, ok = manager.wakeText("Привет, Джарвис, который час")
	if !ok || rest != "который час" {
		t.Fatalf("default wake: rest=%q ok=%v", rest, ok)
	}
}
