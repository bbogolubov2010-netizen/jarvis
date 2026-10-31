package intent

import "testing"

func TestParseRussianWeather(t *testing.T) {
	cases := []struct {
		input  string
		city   string
		period string
	}{
		{"Привет, Джарвис! Какая погода в Москве?", "москве", "today"},
		{"джарвис погода на завтра в санкт-петербурге", "санкт-петербурге", "tomorrow"},
		{"какая погода сейчас", "", "now"},
		{"погода", "", "today"},
		{"Сколько градусов в Москве?", "москве", "today"},
		{"Какая температура в Казани?", "казани", "today"},
		{"Найди в интернете погоду в Москве", "москве", "today"},
		{"Поищи погоду завтра", "", "tomorrow"},
	}
	for _, tc := range cases {
		action, ok := ParseRussian(tc.input)
		if !ok || action.Kind != VoiceWeather {
			t.Fatalf("%q: expected weather, got %#v ok=%v", tc.input, action, ok)
		}
		if action.City != tc.city || action.Period != tc.period {
			t.Fatalf("%q: city=%q period=%q, want city=%q period=%q", tc.input, action.City, action.Period, tc.city, tc.period)
		}
	}
}

func TestParseRussianWake(t *testing.T) {
	for _, input := range []string{"привет Джарвис", "Привет, Джарвис!", "эй джарвис", "Джарвис"} {
		action, ok := ParseRussian(input)
		if !ok || action.Kind != VoiceWake {
			t.Fatalf("%q: expected wake, got %#v ok=%v", input, action, ok)
		}
	}
}

func TestParseRussianActions(t *testing.T) {
	cases := []struct {
		input string
		kind  VoiceActionKind
		arg   string
	}{
		{"открой браузер", VoiceOpenBrowser, ""},
		{"запусти калькулятор", VoiceOpenApp, "калькулятор"},
		{"найди в интернете рецепт борща", VoiceWebSearch, "рецепт борща"},
		{"сделай скриншот", VoiceScreenshot, ""},
		{"выключи компьютер", VoiceShutdown, ""},
		{"перезагрузи компьютер", VoiceRestart, ""},
		{"который час", VoiceTime, ""},
		{"сколько сейчас времени", VoiceTime, ""},
		{"повтори последнее", VoiceRepeat, ""},
		{"стоп", VoiceStop, ""},
		{"стоп, пожалуйста", VoiceStop, ""},
		{"отмена", VoiceCancel, ""},
	}
	for _, tc := range cases {
		action, ok := ParseRussian(tc.input)
		if !ok || action.Kind != tc.kind {
			t.Fatalf("%q: got %#v ok=%v, want %s", tc.input, action, ok, tc.kind)
		}
		if tc.arg != "" && action.Arg != tc.arg && action.Query != tc.arg {
			t.Fatalf("%q: arg=%q query=%q, want %q", tc.input, action.Arg, action.Query, tc.arg)
		}
	}
}

func TestParseEnglishActions(t *testing.T) {
	cases := []struct {
		input string
		kind  VoiceActionKind
		arg   string
	}{
		{"open the browser", VoiceOpenBrowser, ""},
		{"launch notepad", VoiceOpenApp, "notepad"},
		{"start calculator", VoiceOpenApp, "calculator"},
		{"open chrome", VoiceOpenApp, "chrome"},
		{"search the web for best pizza", VoiceWebSearch, "best pizza"},
		{"look up pizza places", VoiceWebSearch, "pizza places"},
		{"google golang generics", VoiceWebSearch, "golang generics"},
		{"take a screenshot", VoiceScreenshot, ""},
		{"shut down the computer", VoiceShutdown, ""},
		{"restart the pc", VoiceRestart, ""},
		{"what time is it", VoiceTime, ""},
		{"what's the date today", VoiceTime, ""},
		{"repeat that", VoiceRepeat, ""},
		{"stop talking", VoiceStop, ""},
		{"be quiet", VoiceStop, ""},
		{"never mind", VoiceCancel, ""},
	}
	for _, tc := range cases {
		action, ok := ParseRussian(tc.input)
		if !ok || action.Kind != tc.kind {
			t.Fatalf("%q: got %#v ok=%v, want %s", tc.input, action, ok, tc.kind)
		}
		if tc.arg != "" && action.Arg != tc.arg && action.Query != tc.arg {
			t.Fatalf("%q: arg=%q query=%q, want %q", tc.input, action.Arg, action.Query, tc.arg)
		}
	}
}

func TestParseEnglishWeather(t *testing.T) {
	cases := []struct {
		input  string
		city   string
		period string
	}{
		{"what's the weather in London", "london", "today"},
		{"weather tomorrow in Paris", "paris", "tomorrow"},
		{"how hot is it right now", "", "now"},
		{"temperature in Berlin", "berlin", "today"},
	}
	for _, tc := range cases {
		action, ok := ParseRussian(tc.input)
		if !ok || action.Kind != VoiceWeather {
			t.Fatalf("%q: expected weather, got %#v ok=%v", tc.input, action, ok)
		}
		if action.City != tc.city || action.Period != tc.period {
			t.Fatalf("%q: city=%q period=%q, want city=%q period=%q", tc.input, action.City, action.Period, tc.city, tc.period)
		}
	}
}

func TestParseEnglishWake(t *testing.T) {
	for _, input := range []string{"hey Jarvis", "hi jarvis", "okay jarvis", "Jarvis"} {
		action, ok := ParseRussian(input)
		if !ok || action.Kind != VoiceWake {
			t.Fatalf("%q: expected wake, got %#v ok=%v", input, action, ok)
		}
	}
}

func TestParseEnglishVolume(t *testing.T) {
	action, ok := ParseRussian("set volume to 40")
	if !ok || action.Kind != VoiceVolume || action.Volume == nil || *action.Volume != 40 {
		t.Fatalf("volume: %#v ok=%v", action, ok)
	}
	action, ok = ParseRussian("turn it down")
	if !ok || action.Volume == nil || *action.Volume != -10 {
		t.Fatalf("quieter: %#v ok=%v", action, ok)
	}
	action, ok = ParseRussian("mute")
	if !ok || action.Mute == nil || !*action.Mute {
		t.Fatalf("mute: %#v ok=%v", action, ok)
	}
}

func TestParseRussianNegatedDangerous(t *testing.T) {
	for _, input := range []string{
		"не выключай компьютер",
		"не перезагружай компьютер",
		"не делай скриншот",
	} {
		action, ok := ParseRussian(input)
		if !ok || action.Kind != VoiceChat {
			t.Fatalf("%q: expected chat, got %#v ok=%v", input, action, ok)
		}
	}
}

func TestParseRussianOpenFullURL(t *testing.T) {
	action, ok := ParseRussian("Открой сайт https://youtube.com.")
	if !ok || action.Kind != VoiceOpenURL {
		t.Fatalf("full URL: got %#v ok=%v", action, ok)
	}
	if action.Arg != "https://youtube.com" {
		t.Fatalf("full URL argument = %q", action.Arg)
	}
}

func TestParseRussianBrowserMentionIsChat(t *testing.T) {
	action, ok := ParseRussian("какой браузер лучше")
	if !ok || action.Kind != VoiceChat {
		t.Fatalf("browser question: got %#v ok=%v", action, ok)
	}
}

func TestParseRussianVolume(t *testing.T) {
	action, ok := ParseRussian("установи громкость 35")
	if !ok || action.Kind != VoiceVolume || action.Volume == nil || *action.Volume != 35 {
		t.Fatalf("volume: %#v ok=%v", action, ok)
	}
	action, ok = ParseRussian("выключи звук")
	if !ok || action.Mute == nil || !*action.Mute {
		t.Fatalf("mute: %#v ok=%v", action, ok)
	}
	action, ok = ParseRussian("сделай громче")
	if !ok || action.Volume == nil || *action.Volume != 10 {
		t.Fatalf("louder: %#v ok=%v", action, ok)
	}
}

func TestParseReminders(t *testing.T) {
	cases := []struct {
		input   string
		minutes int
		at      string
		arg     string
	}{
		{"напомни через 10 минут позвонить маме", 10, "", "позвонить маме"},
		{"поставь таймер на 5 минут", 5, "", "Таймер"},
		{"напомни в 18:30 позвонить", 0, "18:30", "позвонить"},
		{"set a timer for 25 minutes", 25, "", "Таймер"},
		{"remind me in 2 hours to stretch", 120, "", "stretch"},
	}
	for _, tc := range cases {
		action, ok := ParseRussian(tc.input)
		if !ok || action.Kind != VoiceReminder {
			t.Fatalf("%q: got %#v ok=%v, want reminder", tc.input, action, ok)
		}
		if action.Minutes != tc.minutes || action.At != tc.at {
			t.Fatalf("%q: minutes=%d at=%q, want minutes=%d at=%q", tc.input, action.Minutes, action.At, tc.minutes, tc.at)
		}
		if tc.arg != "" && action.Arg != tc.arg {
			t.Fatalf("%q: arg=%q, want %q", tc.input, action.Arg, tc.arg)
		}
	}
}

func TestParseReminderList(t *testing.T) {
	for _, input := range []string{"какие у меня напоминания", "покажи мои напоминания", "list reminders"} {
		action, ok := ParseRussian(input)
		if !ok || action.Kind != VoiceReminders {
			t.Fatalf("%q: got %#v ok=%v, want list_reminders", input, action, ok)
		}
	}
}

func TestParseGameIntents(t *testing.T) {
	action, ok := ParseRussian("во что я играю")
	if !ok || action.Kind != VoiceGameStatus {
		t.Fatalf("game status: got %#v ok=%v", action, ok)
	}
	for _, input := range []string{"запомни эту игру", "это игра", "remember this game"} {
		action, ok := ParseRussian(input)
		if !ok || action.Kind != VoiceGameRemember {
			t.Fatalf("%q: got %#v ok=%v, want game_remember", input, action, ok)
		}
	}
}

func TestConfirmationRussian(t *testing.T) {
	for _, input := range []string{"да", "Да, сэр", "подтверждаю", "так точно"} {
		if !IsConfirmation(input) {
			t.Fatalf("%q should confirm", input)
		}
	}
	for _, input := range []string{"нет", "отмена", "не надо", "отставить"} {
		if !IsCancellation(input) {
			t.Fatalf("%q should cancel", input)
		}
	}
}
