package intent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// VoiceActionKind identifies a deterministic voice/typed Russian intent.
type VoiceActionKind string

const (
	VoiceWake         VoiceActionKind = "wake"
	VoiceWeather      VoiceActionKind = "weather"
	VoiceOpenBrowser  VoiceActionKind = "open_browser"
	VoiceOpenApp      VoiceActionKind = "open_app"
	VoiceOpenURL      VoiceActionKind = "open_url"
	VoiceWebSearch    VoiceActionKind = "web_search"
	VoiceScreenshot   VoiceActionKind = "screenshot"
	VoiceShutdown     VoiceActionKind = "shutdown"
	VoiceRestart      VoiceActionKind = "restart"
	VoiceVolume       VoiceActionKind = "volume"
	VoiceTime         VoiceActionKind = "time"
	VoiceReminder     VoiceActionKind = "reminder"
	VoiceReminders    VoiceActionKind = "list_reminders"
	VoiceGameStatus   VoiceActionKind = "game_status"
	VoiceGameRemember VoiceActionKind = "game_remember"
	VoiceStop         VoiceActionKind = "stop"
	VoiceCancel       VoiceActionKind = "cancel"
	VoiceRepeat       VoiceActionKind = "repeat"
	VoiceChat         VoiceActionKind = "chat"
)

// VoiceAction is the result of parsing a Russian phrase.
type VoiceAction struct {
	Kind   VoiceActionKind
	Text   string
	Arg    string
	Query  string
	City   string
	Period string
	Volume *int
	Mute   *bool
	// Minutes and At describe a reminder or timer delay. At uses 24-hour
	// "HH:MM" and is resolved by the caller against the current clock.
	Minutes int
	At      string
}

var wakeVariants = []string{
	"привет джарвис",
	"привет жарвис",
	"привет дарвис",
	"привет джарвис сэр",
	"эй джарвис",
	"хей джарвис",
	"окей джарвис",
	"ок джарвис",
	"джарвис",
	"жарвис",
	"дарвис",
	"hey jarvis",
	"hi jarvis",
	"ok jarvis",
	"okay jarvis",
	"hey jarvis sir",
	"jarvis",
}

var weatherCityStopWords = map[string]bool{
	"а": true, "в": true, "во": true, "на": true, "с": true, "со": true,
	"сейчас": true, "сегодня": true, "завтра": true, "послезавтра": true,
	"погода": true, "погоду": true, "погоды": true, "погоде": true,
	"какая": true, "какой": true, "какое": true, "как": true,
	"пожалуйста": true, "джарвис": true, "жарвис": true, "дарвис": true,
	"температура": true, "температуру": true, "температуре": true,
	"градус": true, "градуса": true, "градусов": true, "сколько": true,
	"будет": true, "ожидается": true,
	"день": true, "утро": true, "вечер": true, "ночь": true,
	"улице": true, "за": true, "окном": true, "сэр": true,
	"интернет": true, "интернете": true, "сети": true, "сеть": true,
	"найди": true, "найти": true, "поищи": true, "поискать": true,
	"загугли": true, "погугли": true, "поиск": true, "ищи": true,

	// English weather stop words. Apostrophes are removed by normalization,
	// so "what's" arrives as "whats".
	"weather": true, "temperature": true, "forecast": true, "degrees": true,
	"degree": true, "outside": true, "today": true, "tomorrow": true,
	"tonight": true, "now": true, "currently": true, "current": true,
	"what": true, "whats": true, "how": true, "is": true, "it": true,
	"the": true, "in": true, "at": true, "for": true, "of": true,
	"please": true, "jarvis": true, "hot": true, "cold": true,
	"warm": true, "like": true, "will": true, "be": true, "there": true, "right": true,
	"tell": true, "me": true, "give": true, "show": true, "get": true,
}

// NormalizeRussian lowercases Russian and English text, normalizes ё and
// removes punctuation. Apostrophes are dropped rather than replaced with a
// space so English contractions ("what's", "don't") keep one token.
func NormalizeRussian(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "ё", "е")
	text = strings.ReplaceAll(text, "'", "")
	text = strings.ReplaceAll(text, "’", "")
	var b strings.Builder
	b.Grow(len(text))
	lastSpace := false
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-':
			b.WriteRune(r)
			lastSpace = false
		case unicode.IsSpace(r) || unicode.IsPunct(r):
			if !lastSpace && b.Len() > 0 {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// StripWakeWord removes a leading wake phrase and reports whether one was
// present. Vosk often returns punctuation-free text, so comparison is done on
// the normalized form.
func StripWakeWord(text string) (string, bool) {
	normalized := NormalizeRussian(text)
	for _, variant := range wakeVariants {
		if normalized == variant {
			return "", true
		}
		if strings.HasPrefix(normalized, variant+" ") {
			return strings.TrimSpace(normalized[len(variant):]), true
		}
	}
	return normalized, false
}

// IsWakePhrase reports whether the utterance contains only the wake phrase.
func IsWakePhrase(text string) bool {
	rest, hadWake := StripWakeWord(text)
	return hadWake && rest == ""
}

var (
	volumeNumberPattern = regexp.MustCompile(`(?:громкость|звук|volume|sound)\D{0,12}(\d{1,3})`)
	rawURLPattern       = regexp.MustCompile(`(?i)https?://[^\s]+`)
	openPrefixes        = []string{
		"открой", "открыть", "запусти", "запустить", "включи", "включить", "стартуй", "старт",
		"open", "launch", "start", "run",
	}
	searchPrefixes = []string{
		"найди", "найти", "поищи", "поискать", "загугли", "погугли", "поиск", "ищи",
		"search for", "search the web for", "search the internet for", "search online for",
		"search", "look up", "look for", "google", "find online", "find",
	}
	reminderDurationPattern = regexp.MustCompile(`(\d{1,3})\s*(минут[аыу]?|мин|minutes?|mins?|час[аов]*|hours?|hrs?)`)
	// NormalizeRussian turns "18:30" into "18 30", so the separator may be a
	// space as well as a colon or dot.
	reminderClockPattern = regexp.MustCompile(`(?:^|\s)(?:в|at)\s*(\d{1,2})[:.\s](\d{2})(?:\s|$)`)
	reminderPrefixes     = []string{
		"поставь таймер", "засеки", "напомни", "напомнить", "таймер",
		"set a timer", "set timer", "remind me", "reminder", "timer",
	}
)

// ParseRussian recognizes deterministic commands used by voice input and by
// typed Russian messages. It never guesses a dangerous action.
func ParseRussian(text string) (VoiceAction, bool) {
	original := strings.TrimSpace(text)
	if original == "" {
		return VoiceAction{}, false
	}
	rest, hadWake := StripWakeWord(original)
	if rest == "" {
		if hadWake {
			return VoiceAction{Kind: VoiceWake, Text: original}, true
		}
		return VoiceAction{}, false
	}
	n := NormalizeRussian(rest)
	if n == "" {
		if hadWake {
			return VoiceAction{Kind: VoiceWake, Text: original}, true
		}
		return VoiceAction{}, false
	}

	// Wake-only variants such as "привет джарвис сэр".
	if hadWake {
		onlyWake := true
		for _, token := range strings.Fields(n) {
			if token != "сэр" && token != "пожалуйста" {
				onlyWake = false
				break
			}
		}
		if onlyWake {
			return VoiceAction{Kind: VoiceWake, Text: original}, true
		}
	}

	if action, ok := parseReminder(n, original); ok {
		return action, true
	}

	if isGameRememberRequest(n) {
		return VoiceAction{Kind: VoiceGameRemember, Text: original}, true
	}

	if isGameStatusRequest(n) {
		return VoiceAction{Kind: VoiceGameStatus, Text: original}, true
	}

	if strings.Contains(n, "погод") || strings.Contains(n, "температур") || strings.Contains(n, "градус") ||
		isEnglishWeatherRequest(n) {
		return VoiceAction{
			Kind:   VoiceWeather,
			Text:   original,
			City:   extractWeatherCity(n),
			Period: extractWeatherPeriod(n),
		}, true
	}

	if isBrowserRequest(n) {
		return VoiceAction{Kind: VoiceOpenBrowser, Text: original}, true
	}

	if action, ok := parseOpenURL(n, original); ok {
		return action, true
	}

	if action, ok := parseVolume(n, original); ok {
		return action, true
	}

	if isTimeRequest(n) {
		return VoiceAction{Kind: VoiceTime, Text: original}, true
	}

	if isScreenshotRequest(n) {
		return VoiceAction{Kind: VoiceScreenshot, Text: original}, true
	}

	if isShutdownRequest(n) {
		return VoiceAction{Kind: VoiceShutdown, Text: original}, true
	}

	if isRestartRequest(n) {
		return VoiceAction{Kind: VoiceRestart, Text: original}, true
	}

	if isStopRequest(n) {
		return VoiceAction{Kind: VoiceStop, Text: original}, true
	}

	if isCancelRequest(n) {
		return VoiceAction{Kind: VoiceCancel, Text: original}, true
	}

	if isRepeatRequest(n) {
		return VoiceAction{Kind: VoiceRepeat, Text: original}, true
	}

	if query, ok := parseSearch(n); ok {
		return VoiceAction{Kind: VoiceWebSearch, Text: original, Query: query}, true
	}

	if name, ok := parseOpenApp(n); ok {
		return VoiceAction{Kind: VoiceOpenApp, Text: original, Arg: name}, true
	}

	return VoiceAction{Kind: VoiceChat, Text: original, Query: n}, true
}

func isGameRememberRequest(n string) bool {
	for _, phrase := range []string{
		"запомни игру", "запомни эту игру", "это игра", "добавь игру", "добавить игру",
		"remember this game", "remember the game", "add this game", "this is a game",
	} {
		if strings.Contains(n, phrase) {
			return true
		}
	}
	return false
}

func isGameStatusRequest(n string) bool {
	for _, phrase := range []string{
		"во что я играю", "в какую игру", "какая игра", "какую игру я", "сколько я играю",
		"сколько уже играю", "сколько времени я играю", "статус игры", "моя игра",
		"what am i playing", "which game", "what game", "game status",
		"how long have i been playing", "how long am i playing",
	} {
		if strings.Contains(n, phrase) {
			return true
		}
	}
	return false
}

func parseReminder(n, original string) (VoiceAction, bool) {
	// "какие у меня напоминания" / "list reminders" is a read-only query and
	// does not use a reminder verb, so check it before prefix matching.
	if isReminderListRequest(n) {
		return VoiceAction{Kind: VoiceReminders, Text: original}, true
	}

	prefix, ok := reminderPrefix(n)
	if !ok {
		return VoiceAction{}, false
	}

	action := VoiceAction{Kind: VoiceReminder, Text: original}
	if match := reminderClockPattern.FindStringSubmatch(n); len(match) == 3 {
		hour, hourErr := strconv.Atoi(match[1])
		minute, minuteErr := strconv.Atoi(match[2])
		if hourErr == nil && minuteErr == nil && hour < 24 && minute < 60 {
			action.At = fmt.Sprintf("%02d:%02d", hour, minute)
		}
	}
	if action.At == "" {
		action.Minutes = reminderMinutes(n)
	}

	text := n
	text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	text = reminderDurationPattern.ReplaceAllString(text, " ")
	text = reminderClockPattern.ReplaceAllString(text, " ")
	text = trimReminderFillers(text)
	if text == "" {
		text = "Таймер"
	}
	action.Arg = text
	return action, true
}

func isReminderListRequest(n string) bool {
	if strings.Contains(n, "list reminders") || strings.Contains(n, "my reminders") || n == "reminders" {
		return true
	}
	if !strings.Contains(n, "напоминани") {
		return false
	}
	for _, marker := range []string{"какие", "мои", "список", "покажи", "перечисли"} {
		if strings.Contains(n, marker) {
			return true
		}
	}
	return false
}

func reminderPrefix(n string) (string, bool) {
	for _, prefix := range reminderPrefixes {
		if n == prefix || strings.HasPrefix(n, prefix+" ") {
			return prefix, true
		}
	}
	return "", false
}

func reminderMinutes(n string) int {
	switch {
	case strings.Contains(n, "полтора часа") || strings.Contains(n, "полтора час"):
		return 90
	case strings.Contains(n, "полчаса") || strings.Contains(n, "half an hour"):
		return 30
	}
	match := reminderDurationPattern.FindStringSubmatch(n)
	if len(match) != 3 {
		return 0
	}
	value, err := strconv.Atoi(match[1])
	if err != nil || value <= 0 {
		return 0
	}
	unit := match[2]
	if strings.HasPrefix(unit, "час") || strings.HasPrefix(unit, "hour") || strings.HasPrefix(unit, "hr") {
		return value * 60
	}
	return value
}

func trimReminderFillers(text string) string {
	fields := strings.Fields(text)
	for len(fields) > 0 {
		switch fields[0] {
		case "мне", "через", "на", "in", "to", "for", "that", "что", "пожалуйста", "please":
			fields = fields[1:]
		default:
			return strings.Join(fields, " ")
		}
	}
	return ""
}

func extractWeatherPeriod(n string) string {
	switch {
	case strings.Contains(n, "послезавтра"):
		return "after_tomorrow"
	case strings.Contains(n, "завтра"):
		return "tomorrow"
	case strings.Contains(n, "сейчас") || strings.Contains(n, "текущ"):
		return "now"
	case strings.Contains(n, "tomorrow"):
		return "tomorrow"
	case strings.Contains(n, "right now") || strings.Contains(n, "currently") || strings.Contains(n, "current"):
		return "now"
	default:
		return "today"
	}
}

// isEnglishWeatherRequest recognizes common English weather questions.
func isEnglishWeatherRequest(n string) bool {
	for _, phrase := range []string{
		"weather", "temperature", "forecast", "how hot", "how cold",
		"degrees outside", "whats the temp", "what is the temp",
	} {
		if strings.Contains(n, phrase) {
			return true
		}
	}
	return false
}

func extractWeatherCity(n string) string {
	words := strings.Fields(n)
	city := make([]string, 0, 4)
	for _, word := range words {
		if weatherCityStopWords[word] {
			continue
		}
		if strings.Contains(word, "погод") || strings.Contains(word, "температур") {
			continue
		}
		city = append(city, word)
	}
	return strings.TrimSpace(strings.Join(city, " "))
}

func isBrowserRequest(n string) bool {
	if n == "браузер" || n == "browser" {
		return true
	}
	if strings.Contains(n, "браузер") || strings.Contains(n, "browser") {
		for _, prefix := range openPrefixes {
			if strings.HasPrefix(n, prefix+" ") {
				return true
			}
		}
	}
	for _, phrase := range []string{
		"открой интернет", "запусти интернет",
		"open the internet", "open internet", "open the web", "open web",
		"open website", "open the website", "open site", "open the site",
	} {
		if n == phrase {
			return true
		}
	}
	return false
}

func parseOpenURL(n, original string) (VoiceAction, bool) {
	for _, prefix := range []string{
		"открой сайт", "открой страницу", "перейди на", "открой ссылку",
		"open the website", "open website", "open the site", "open site",
		"open the page", "open page", "open the link", "open link",
		"go to", "navigate to", "visit",
	} {
		if strings.HasPrefix(n, prefix) {
			// Normalization removes the punctuation in a full URL, so prefer
			// the untouched address from the original utterance when present.
			if match := rawURLPattern.FindString(original); match != "" {
				match = strings.TrimRight(match, ".,;!?)]}»\"'")
				return VoiceAction{Kind: VoiceOpenURL, Text: original, Arg: match}, true
			}
			target := strings.TrimSpace(strings.TrimPrefix(n, prefix))
			target = strings.TrimSpace(strings.TrimPrefix(target, "the "))
			if target == "" {
				// "open website" without a target is a plain browser request.
				if strings.Contains(prefix, "website") || strings.Contains(prefix, "site") ||
					strings.Contains(prefix, "page") {
					return VoiceAction{Kind: VoiceOpenBrowser, Text: original}, true
				}
				return VoiceAction{}, false
			}
			if !strings.HasPrefix(target, "http") {
				target = "https://" + strings.ReplaceAll(target, " ", "")
			}
			return VoiceAction{Kind: VoiceOpenURL, Text: original, Arg: target}, true
		}
	}
	return VoiceAction{}, false
}

func parseVolume(n, original string) (VoiceAction, bool) {
	if match := volumeNumberPattern.FindStringSubmatch(n); len(match) == 2 {
		value, err := strconv.Atoi(match[1])
		if err == nil && value >= 0 && value <= 100 {
			return VoiceAction{Kind: VoiceVolume, Text: original, Volume: &value}, true
		}
	}
	if strings.Contains(n, "выключи звук") || strings.Contains(n, "без звука") || strings.Contains(n, "мьют") ||
		n == "mute" || strings.Contains(n, "mute the") || strings.Contains(n, "mute sound") {
		muted := true
		return VoiceAction{Kind: VoiceVolume, Text: original, Mute: &muted}, true
	}
	if strings.Contains(n, "включи звук") || strings.Contains(n, "верни звук") || strings.Contains(n, "unmute") ||
		strings.Contains(n, "turn the sound on") || strings.Contains(n, "sound on") {
		muted := false
		return VoiceAction{Kind: VoiceVolume, Text: original, Mute: &muted}, true
	}
	if strings.Contains(n, "громче") || strings.Contains(n, "прибавь звук") || strings.Contains(n, "увеличь звук") ||
		strings.Contains(n, "louder") || strings.Contains(n, "volume up") || strings.Contains(n, "turn it up") ||
		strings.Contains(n, "increase volume") || strings.Contains(n, "raise the volume") {
		delta := 10
		return VoiceAction{Kind: VoiceVolume, Text: original, Volume: &delta}, true
	}
	if strings.Contains(n, "тише") || strings.Contains(n, "убавь звук") || strings.Contains(n, "уменьши звук") ||
		strings.Contains(n, "quieter") || strings.Contains(n, "volume down") || strings.Contains(n, "turn it down") ||
		strings.Contains(n, "decrease volume") || strings.Contains(n, "lower the volume") {
		delta := -10
		return VoiceAction{Kind: VoiceVolume, Text: original, Volume: &delta}, true
	}
	return VoiceAction{}, false
}

func isTimeRequest(n string) bool {
	for _, phrase := range []string{
		"который час", "который сейчас час", "сколько времени", "сколько сейчас времени",
		"сколько время", "текущее время", "какое время", "какое сейчас время",
		"скажи время", "подскажи время", "какая дата", "какое сегодня число",
		"какой сегодня день",
		"what time", "whats the time", "what is the time", "current time", "the time please",
		"tell me the time", "whats the date", "what is the date", "todays date",
		"what day is it", "whats today", "what is todays date",
	} {
		if strings.Contains(n, phrase) {
			return true
		}
	}
	return n == "время" || n == "дата" || n == "число" || n == "time" || n == "date"
}

func isScreenshotRequest(n string) bool {
	if hasNegation(n) {
		return false
	}
	return strings.Contains(n, "скриншот") || strings.Contains(n, "скрин") ||
		strings.Contains(n, "снимок экрана") || strings.Contains(n, "сфотографируй экран") ||
		strings.Contains(n, "screenshot") || strings.Contains(n, "screen shot") ||
		strings.Contains(n, "screen capture") || strings.Contains(n, "capture the screen") ||
		strings.Contains(n, "capture screen")
}

func isShutdownRequest(n string) bool {
	if hasNegation(n) {
		return false
	}
	if strings.Contains(n, "заверши работу") || strings.Contains(n, "выключи систему") || strings.Contains(n, "выключи компьютер") {
		return true
	}
	if strings.Contains(n, "выключи") && (strings.Contains(n, "пк") || strings.Contains(n, "комп") || strings.Contains(n, "windows")) {
		return true
	}
	for _, phrase := range []string{
		"power off", "shutdown", "shut down", "turn off the computer",
		"turn off the pc", "turn off the machine", "shut the computer down",
	} {
		if strings.Contains(n, phrase) {
			return true
		}
	}
	return false
}

func isRestartRequest(n string) bool {
	if hasNegation(n) {
		return false
	}
	return strings.Contains(n, "перезагрузи") || strings.Contains(n, "перезагрузка") ||
		strings.Contains(n, "рестарт") || strings.Contains(n, "restart") || strings.Contains(n, "reboot") ||
		strings.Contains(n, "reboot the computer")
}

// hasNegation prevents "не выключай компьютер" from being treated as a
// shutdown request. The parser only needs a conservative guard here; a negated
// dangerous phrase falls through to chat instead of prompting for confirmation.
func hasNegation(n string) bool {
	for _, token := range strings.Fields(n) {
		switch token {
		case "не", "нельзя", "запрещено", "not", "dont", "never":
			return true
		}
	}
	return false
}

func isStopRequest(n string) bool {
	switch n {
	case "стоп", "остановись", "замолчи", "хватит", "тихо", "стоп джарвис", "stop",
		"quiet", "be quiet", "silence", "enough", "shut up":
		return true
	}
	for _, prefix := range []string{
		"стоп ", "остановись ", "замолчи ", "хватит ",
		"stop ", "quiet ", "be quiet ", "silence ", "stop talking",
	} {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return strings.HasPrefix(n, "останови") && (strings.Contains(n, "ответ") || strings.Contains(n, "речь"))
}

func isCancelRequest(n string) bool {
	switch n {
	case "отмена", "отмени", "не надо", "не нужно", "нет", "cancel", "never mind",
		"nevermind", "forget it", "abort":
		return true
	}
	return strings.Contains(n, "отмени действие")
}

func isRepeatRequest(n string) bool {
	for _, phrase := range []string{
		"повтори", "еще раз", "ещё раз", "что ты сказал", "повтори последнее",
		"repeat", "say that again", "say it again", "what did you say", "come again",
	} {
		if strings.Contains(n, phrase) {
			return true
		}
	}
	return false
}

func parseSearch(n string) (string, bool) {
	for _, prefix := range searchPrefixes {
		if n == prefix {
			return "", false
		}
		if strings.HasPrefix(n, prefix+" ") {
			query := strings.TrimSpace(strings.TrimPrefix(n, prefix))
			for _, filler := range []string{
				"в интернете", "в сети",
				"the web for", "the internet for", "online for", "for", "the",
				"on the internet", "on the web", "online",
			} {
				query = strings.TrimSpace(strings.TrimPrefix(query, filler))
			}
			if query != "" {
				return query, true
			}
		}
	}
	return "", false
}

func parseOpenApp(n string) (string, bool) {
	for _, prefix := range openPrefixes {
		if n == prefix {
			return "", false
		}
		if strings.HasPrefix(n, prefix+" ") {
			name := strings.TrimSpace(strings.TrimPrefix(n, prefix))
			for _, filler := range []string{
				"программу", "приложение", "the app", "the application", "the program",
				"application", "program", "app", "up",
			} {
				name = strings.TrimSpace(strings.TrimPrefix(name, filler))
			}
			if name == "" {
				return "", false
			}
			return name, true
		}
	}
	return "", false
}
