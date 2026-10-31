package voiceinput

import "testing"

func TestCleanTranscriptFiltersHallucinations(t *testing.T) {
	for _, text := range []string{
		"Продолжение следует...",
		"Субтитры",
		"Редактор субтитров А. Сидоров",
		"Спасибо за просмотр!",
		"Музыка",
		"...",
		"Thank you for watching!",
		"Please subscribe",
		"Subtitles by Amara.org",
		"Applause",
		"Silence",
	} {
		if got := cleanTranscript(text); got != "" {
			t.Errorf("cleanTranscript(%q) = %q, want empty", text, got)
		}
	}
	for _, text := range []string{
		"Привет, Джарвис. Какая погода в Москве!",
		"Hey Jarvis, open the browser",
		"What is the weather in London?",
	} {
		if got := cleanTranscript(text); got == "" {
			t.Errorf("valid speech %q was filtered", text)
		}
	}
}

func TestTranscriberLanguage(t *testing.T) {
	cases := map[string]string{
		"":     "auto",
		"auto": "auto",
		"ru":   "ru",
		"EN":   "en",
		"xx":   "auto",
	}
	for input, want := range cases {
		transcriber := &Transcriber{Language: input}
		if got := transcriber.language(); got != want {
			t.Errorf("language(%q) = %q, want %q", input, got, want)
		}
	}
}
