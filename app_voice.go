package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"Jarvis/internal/audio"
	"Jarvis/internal/audit"
	"Jarvis/internal/browser"
	"Jarvis/internal/commands"
	"Jarvis/internal/config"
	"Jarvis/internal/intent"
	"Jarvis/internal/ollama"
	"Jarvis/internal/sanitize"
	"Jarvis/internal/voice"
	"Jarvis/internal/voiceengine"
	"Jarvis/internal/voiceinput"
	"Jarvis/internal/weather"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// VoiceStatus is the settings-screen snapshot of the voice subsystem.
type VoiceStatus struct {
	Enabled  bool               `json:"enabled"`
	State    string             `json:"state"`
	WakeWord string             `json:"wake_word"`
	City     string             `json:"city"`
	Engine   voiceengine.Status `json:"engine"`
	Input    voiceinput.Status  `json:"input"`
}

func (a *App) voiceInputDisabled() bool {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.settings.VoiceInputDisabled
}

func (a *App) wakeWordDisabled() bool {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.settings.WakeWordDisabled
}

func (a *App) voiceConfirmDangerous() bool {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return !a.settings.VoiceConfirmDangerousDisabled
}

func (a *App) voiceManager() *voiceinput.Manager {
	a.voiceMu.Lock()
	defer a.voiceMu.Unlock()
	return a.voiceInput
}

var errVoiceStartCanceled = errors.New("voice input start canceled")

func (a *App) voiceContext() context.Context {
	a.voiceCommandMu.Lock()
	commandCtx := a.voiceCommandCtx
	a.voiceCommandMu.Unlock()
	if commandCtx != nil {
		return commandCtx
	}
	return a.voiceBaseContext()
}

func (a *App) voiceBaseContext() context.Context {
	if a.voiceCtx != nil {
		return a.voiceCtx
	}
	return context.Background()
}

// beginVoiceCommand gives one spoken command a cancellable context. Stopping
// voice input cancels the context so a slow model request cannot make the
// microphone shutdown wait for its full timeout.
func (a *App) beginVoiceCommand() func() {
	ctx, cancel := context.WithCancel(a.voiceContext())

	a.voiceCommandMu.Lock()
	if a.voiceCommandStop != nil {
		a.voiceCommandStop()
	}
	a.voiceCommandCtx = ctx
	a.voiceCommandStop = cancel
	a.voiceCommandMu.Unlock()

	return func() {
		cancel()
		a.voiceCommandMu.Lock()
		if a.voiceCommandCtx == ctx {
			a.voiceCommandCtx = nil
			a.voiceCommandStop = nil
		}
		a.voiceCommandMu.Unlock()
	}
}

func (a *App) cancelVoiceCommand() {
	a.voiceCommandMu.Lock()
	cancel := a.voiceCommandStop
	a.voiceCommandMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// verifyVoiceEngine checks the checksum manifest the first time a given engine
// root is used. Re-resolving the engine on every call lets a user install or
// replace it while JARVIS is open without restarting the app.
func (a *App) verifyVoiceEngine(paths voiceengine.Paths) error {
	a.voiceMu.Lock()
	defer a.voiceMu.Unlock()
	a.voicePaths = paths
	if a.voiceEngineVerified && a.voiceEngineRoot == paths.Root {
		if a.voiceEngineError != "" {
			return errors.New(a.voiceEngineError)
		}
		return nil
	}
	a.voiceEngineRoot = paths.Root
	a.voiceEngineVerified = true
	if err := paths.Verify(); err != nil {
		a.voiceEngineError = err.Error()
		return err
	}
	a.voiceEngineError = ""
	return nil
}

func (a *App) startVoiceInput() error {
	if a.voiceInputDisabled() {
		return nil
	}
	a.voiceMu.Lock()
	if a.voiceStarting || a.voiceInput != nil {
		a.voiceMu.Unlock()
		return nil
	}
	a.voiceMu.Unlock()

	// Checksum verification can read tens of megabytes, so run it before the
	// "starting" flag is raised. The helper caches the result per engine root.
	paths := voiceengine.Resolve()
	if err := a.verifyVoiceEngine(paths); err != nil {
		return fmt.Errorf("voice engine integrity check failed: %w", err)
	}

	a.voiceMu.Lock()
	if a.voiceStarting || a.voiceInput != nil {
		a.voiceMu.Unlock()
		return nil
	}
	a.voiceStarting = true
	a.voiceMu.Unlock()
	settings := a.GetSettings()

	callbacks := voiceinput.Callbacks{
		OnState: func(state voiceinput.State) {
			a.emitEvent("voice:state", map[string]any{"state": string(state)})
		},
		OnLevel: func(level float64) {
			a.emitEvent("voice:level", map[string]any{"level": level})
		},
		OnWake: func(string) {
			a.onVoiceWake()
		},
		OnCommand: func(text string) bool {
			return a.onVoiceCommand(text)
		},
		OnTranscript: func(text string) {
			cleaned := strings.TrimSpace(sanitize.SanitizeUserInput(text).Content)
			if cleaned == "" {
				return
			}
			a.emitEvent("voice:transcript", map[string]any{"text": cleaned, "final": true})
		},
		OnVosk: func(text string) {
			a.emitEvent("voice:heard", map[string]any{"text": text})
		},
		OnError: func(message string) {
			a.emitEvent("voice:error", map[string]any{"message": message})
		},
	}

	manager := voiceinput.NewManager(paths, settings.VoiceDeviceID, callbacks)
	manager.SetWakeWord(settings.VoiceWakeWord)
	manager.SetWakeWordDisabled(settings.WakeWordDisabled)
	manager.SetLanguage(settings.VoiceLanguage)
	var err error
	for attempt := 0; attempt < 25; attempt++ {
		a.voiceMu.Lock()
		starting := a.voiceStarting
		a.voiceMu.Unlock()
		if !starting {
			break
		}
		err = manager.Start()
		if err == nil {
			break
		}
		// The background agent may still be releasing the microphone during
		// a cold-start handoff. Retry briefly before reporting a failure.
		time.Sleep(400 * time.Millisecond)
	}

	a.voiceMu.Lock()
	if !a.voiceStarting {
		// StopVoiceInput was called while the model and microphone were
		// starting. Do not resurrect the listener.
		a.voiceMu.Unlock()
		manager.Stop()
		return errVoiceStartCanceled
	}
	a.voiceStarting = false
	if err != nil {
		a.voiceInput = nil
		a.voiceMu.Unlock()
		a.emitEvent("voice:error", map[string]any{"message": err.Error()})
		return err
	}
	a.voiceInput = manager
	a.voiceMu.Unlock()
	a.emitEvent("voice:state", map[string]any{"state": string(voiceinput.StateWake)})
	return nil
}

func (a *App) stopVoiceInput() {
	a.cancelVoiceCommand()
	a.voiceMu.Lock()
	manager := a.voiceInput
	a.voiceInput = nil
	a.voiceStarting = false
	a.voiceMu.Unlock()
	if manager != nil {
		manager.Stop()
	}
}

// interruptVoicePlayback stops TTS and phrase playback so an explicit voice
// toggle does not wait for a long spoken reply.
func (a *App) interruptVoicePlayback() {
	if a.voice != nil {
		a.voice.Stop()
	}
}

func (a *App) restartVoiceInput() error {
	// A settings change can arrive while JARVIS is speaking. Stop playback
	// first so Stop does not wait for the command callback to finish.
	a.interruptVoicePlayback()
	a.stopVoiceInput()
	if a.voiceInputDisabled() {
		return nil
	}
	return a.startVoiceInput()
}

// startVoiceInputWithRetry keeps trying after a logon start. Windows may still
// be starting the audio service when the Run entry fires, and a single failure
// must not leave JARVIS deaf until the next manual restart.
func (a *App) startVoiceInputWithRetry() {
	backoff := 5 * time.Second
	for attempt := 0; attempt < 6; attempt++ {
		if a.voiceInputDisabled() || a.voiceManager() != nil {
			return
		}
		err := a.startVoiceInput()
		if err == nil && a.voiceManager() != nil {
			return
		}
		if err != nil && !errors.Is(err, errVoiceStartCanceled) {
			agentLog("voice input start (attempt %d): %v", attempt+1, err)
		}
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff += 5 * time.Second
		}
	}
}

func (a *App) onVoiceWake() {
	a.showWindowSilent()
	a.emitEvent("voice:wake", map[string]any{"text": "wake"})
	if a.voice != nil {
		_ = a.voice.PlaySync(voice.KeyAck)
	}
}

func (a *App) onVoiceCommand(text string) bool {
	// The manager and the single-instance handoff can both deliver a command.
	// Serialize them so history and the active command context stay coherent.
	a.voiceCommandRun.Lock()
	defer a.voiceCommandRun.Unlock()

	sanitized := sanitize.SanitizeUserInput(text)
	if sanitized.HadInjection {
		audit.LogInjectionBlocked("voice input")
	}
	text = strings.TrimSpace(sanitized.Content)
	if text == "" {
		return false
	}
	finishCommand := a.beginVoiceCommand()
	defer finishCommand()

	userMessage := ollama.Message{Role: "user", Content: text}
	a.appendHistory(userMessage)

	display, followUp := a.handleVoiceText(text)
	if a.voiceContext().Err() != nil {
		// The user stopped voice input while the command was running. Drop the
		// unfinished turn instead of persisting a misleading error reply.
		a.rollbackUserMessage(userMessage)
		return false
	}
	if display != "" {
		a.appendHistory(ollama.Message{Role: "assistant", Content: display})
		a.setLastAssistant(display)
		a.emitEvent("voice:message", map[string]any{"role": "assistant", "content": display})
	}
	return followUp
}

func (a *App) handleVoiceText(text string) (display string, followUp bool) {
	if rest, hadWake := a.stripConfiguredWake(text); hadWake {
		if rest == "" {
			return "Да, сэр?", true
		}
		text = rest
	}
	if pending, confirmed := a.intents.TakeConfirmation(text); confirmed {
		result := a.runConfirmed(pending)
		display = a.describeConfirmed(pending, result)
		return display, false
	}
	if a.intents.Has() && intent.IsCancellation(text) {
		a.intents.Clear()
		return "Отменено, сэр.", false
	}
	if a.intents.Has() {
		a.intents.Clear()
	}

	action, ok := intent.ParseRussian(text)
	if !ok {
		return "Не расслышал, сэр.", false
	}

	switch action.Kind {
	case intent.VoiceWake:
		return "Да, сэр?", true
	case intent.VoiceWeather:
		return a.voiceWeather(action)
	case intent.VoiceOpenBrowser:
		return a.voiceOpenBrowser()
	case intent.VoiceOpenURL:
		return a.voiceOpenURL(action.Arg)
	case intent.VoiceOpenApp:
		return a.voiceOpenApp(action.Arg)
	case intent.VoiceWebSearch:
		return a.voiceWebSearch(action.Query)
	case intent.VoiceScreenshot:
		return a.voiceDangerous(intent.Screenshot, "", "Сделать снимок экрана?")
	case intent.VoiceShutdown:
		return a.voiceDangerous(intent.Shutdown, "", "Выключить компьютер?")
	case intent.VoiceRestart:
		return a.voiceDangerous(intent.Restart, "", "Перезагрузить компьютер?")
	case intent.VoiceVolume:
		return a.voiceVolume(action)
	case intent.VoiceTime:
		return a.voiceTime(action.Text)
	case intent.VoiceReminder:
		return a.voiceReminder(action)
	case intent.VoiceReminders:
		return a.voiceListReminders()
	case intent.VoiceGameStatus:
		return a.voiceGameStatus()
	case intent.VoiceGameRemember:
		return a.voiceGameRemember()
	case intent.VoiceStop:
		if a.voice != nil {
			a.voice.Stop()
		}
		// The user asked for silence, so the reply stays on screen only.
		return "Останавливаюсь, сэр.", false
	case intent.VoiceCancel:
		a.intents.Clear()
		return "Отменено, сэр.", false
	case intent.VoiceRepeat:
		last := a.getLastAssistant()
		if last == "" {
			return "Мне нечего повторять, сэр.", false
		}
		return last, false
	case intent.VoiceChat:
		query := action.Query
		if query == "" {
			query = action.Text
		}
		return a.voiceChat(query)
	default:
		return "Не расслышал, сэр.", false
	}
}

// stripConfiguredWake removes a custom wake phrase before intent parsing.
// The built-in phrases are already handled by intent.StripWakeWord.
func (a *App) stripConfiguredWake(text string) (string, bool) {
	phrase := intent.NormalizeRussian(a.GetSettings().VoiceWakeWord)
	if phrase == "" || phrase == intent.NormalizeRussian("привет джарвис") {
		return text, false
	}
	normalized := intent.NormalizeRussian(text)
	if normalized == phrase {
		return "", true
	}
	if strings.HasPrefix(normalized, phrase+" ") {
		return strings.TrimSpace(normalized[len(phrase):]), true
	}
	return text, false
}

func (a *App) voiceDangerous(kind intent.Kind, arg, prompt string) (string, bool) {
	if a.voiceConfirmDangerous() {
		a.intents.Set(kind, arg)
		text := "Сэр, " + prompt + " Скажите «да» для подтверждения."
		return text, true
	}
	result := a.runConfirmed(intent.Pending{Kind: kind, Arg: arg})
	display := a.describeConfirmed(intent.Pending{Kind: kind, Arg: arg}, result)
	return display, false
}

func (a *App) describeConfirmed(pending intent.Pending, result string) string {
	// Do not treat arbitrary search snippets containing "not found" as an
	// action failure. Only the command result's own error prefixes count.
	failed := looksLikeError(result) || strings.HasPrefix(
		strings.ToLower(strings.TrimSpace(result)),
		"nothing to confirm",
	)
	if failed {
		return result
	}
	if looksLikeNoResults(result) {
		return result
	}
	switch pending.Kind {
	case intent.Shutdown:
		return "Выключаю компьютер через пять секунд, сэр."
	case intent.Restart:
		return "Перезагружаю компьютер через пять секунд, сэр."
	case intent.Screenshot:
		return result
	case intent.WebSearch, intent.SmartSearch:
		return result
	case intent.OpenApp:
		return result
	default:
		return result
	}
}

func (a *App) voiceWeather(action intent.VoiceAction) (string, bool) {
	settings := a.GetSettings()
	requestedCity := strings.TrimSpace(action.City)
	city := requestedCity
	if city == "" {
		city = strings.TrimSpace(settings.VoiceCity)
	}
	period := action.Period
	if period == "" {
		period = "today"
	}
	ctx, cancel := context.WithTimeout(a.voiceContext(), 20*time.Second)
	defer cancel()

	// Reuse the coordinates stored by the first-launch prompt. This avoids a
	// redundant geocoding request and keeps the forecast tied to the city the
	// user actually chose, even if the geocoder later resolves a different
	// settlement with the same name.
	usedStoredLocation := requestedCity == "" && city != "" &&
		(settings.VoiceLatitude != 0 || settings.VoiceLongitude != 0)
	var (
		forecast weather.Forecast
		err      error
	)
	if usedStoredLocation {
		loc := weather.Location{
			Name:      settings.VoiceCity,
			Latitude:  settings.VoiceLatitude,
			Longitude: settings.VoiceLongitude,
		}
		forecast, err = a.weather.GetForLocation(ctx, loc, period)
		if err != nil {
			// Stored coordinates can be stale after a settings edit; fall back
			// to geocoding instead of failing the whole request.
			forecast, err = a.weather.Get(ctx, city, period)
		}
	} else {
		forecast, err = a.weather.Get(ctx, city, period)
	}
	if err != nil {
		message := "Не удалось получить прогноз погоды, сэр. " + err.Error()
		return message, false
	}

	firstLocation := !settings.VoiceLocationAsked && requestedCity == "" && city == ""
	if forecast.Location.Name != "" {
		_ = a.updateVoiceSettings(func(s *config.Settings) {
			s.VoiceCity = forecast.Location.Name
			s.VoiceLatitude = forecast.Location.Latitude
			s.VoiceLongitude = forecast.Location.Longitude
			s.VoiceLocationAsked = true
		})
	}

	display := forecast.Markdown()
	if firstLocation {
		display = "Сэр, я определил ваше местоположение: " + forecast.Location.Name + ".\n\n" + display
	}
	return display, false
}

func (a *App) voiceOpenBrowser() (string, bool) {
	if a.ctx == nil {
		return "Приложение ещё не готово, сэр.", false
	}
	wruntime.BrowserOpenURL(a.ctx, "https://www.google.com")
	return "Открываю браузер, сэр.", false
}

func (a *App) voiceOpenURL(rawURL string) (string, bool) {
	if a.ctx == nil {
		return "Приложение ещё не готово, сэр.", false
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return "Некорректная ссылка, сэр.", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if parsed.Host == "" || (scheme != "http" && scheme != "https") {
		return "Некорректная ссылка, сэр.", false
	}
	if !browser.IsPublicURL(a.ctx, parsed.String()) {
		return "Локальные и частные адреса открывать нельзя, сэр.", false
	}
	wruntime.BrowserOpenURL(a.ctx, parsed.String())
	return "Открываю ссылку, сэр.", false
}

func (a *App) voiceOpenApp(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Какое приложение открыть, сэр?", false
	}
	if target, ok := voiceSiteURLs[strings.ToLower(name)]; ok {
		return a.voiceOpenURL(target)
	}
	audit.LogOpen(name)
	result := commands.LaunchByName(name)
	if looksLikeError(result) {
		message := fmt.Sprintf("Не удалось открыть %s, сэр.", name)
		return message, false
	}
	message := fmt.Sprintf("Открываю %s, сэр.", name)
	return message, false
}

func (a *App) voiceWebSearch(query string) (string, bool) {
	if !a.webSearchEnabled() {
		return "Поиск в интернете отключён в настройках, сэр.", false
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "Что найти, сэр?", false
	}
	result := a.runConfirmed(intent.Pending{Kind: intent.WebSearch, Arg: query})
	if looksLikeError(result) {
		return result, false
	}
	if looksLikeNoResults(result) {
		return result, false
	}
	return result, false
}

func (a *App) voiceVolume(action intent.VoiceAction) (string, bool) {
	if action.Mute != nil {
		if err := audio.SetMuted(*action.Mute); err != nil {
			message := "Не удалось изменить звук, сэр."
			return message, false
		}
		if *action.Mute {
			return "Звук выключен, сэр.", false
		}
		return "Звук включён, сэр.", false
	}
	if action.Volume == nil {
		return "Не понял команду громкости, сэр.", false
	}
	text := strings.ToLower(action.Text)
	relative := strings.Contains(text, "громче") || strings.Contains(text, "тише") ||
		strings.Contains(text, "прибавь") || strings.Contains(text, "убавь") ||
		strings.Contains(text, "увеличь") || strings.Contains(text, "уменьши")
	var (
		level int
		err   error
	)
	if relative {
		level, err = audio.ChangeVolume(*action.Volume)
	} else {
		level = *action.Volume
		err = audio.SetVolumePercent(level)
	}
	if err != nil {
		message := "Не удалось изменить громкость, сэр."
		return message, false
	}
	message := fmt.Sprintf("Громкость %d процентов, сэр.", level)
	return message, false
}

func (a *App) voiceTime(text string) (string, bool) {
	lower := strings.ToLower(text)
	now := time.Now()
	if strings.Contains(lower, "дата") || strings.Contains(lower, "число") || strings.Contains(lower, "день") {
		message := "Сегодня " + russianDate(now) + ", сэр."
		return message, false
	}
	message := "Сейчас " + russianTime(now) + ", сэр."
	return message, false
}

func russianTime(now time.Time) string {
	return fmt.Sprintf("%d %s %d %s",
		now.Hour(), russianPlural(now.Hour(), "час", "часа", "часов"),
		now.Minute(), russianPlural(now.Minute(), "минута", "минуты", "минут"))
}

func russianPlural(n int, one, few, many string) string {
	n %= 100
	if n >= 11 && n <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

func (a *App) voiceChat(query string) (string, bool) {
	sanitized := sanitize.SanitizeUserInput(query)
	if sanitized.HadInjection {
		message := "Я не могу выполнить этот запрос, сэр."
		return message, false
	}
	content := strings.TrimSpace(sanitized.Content)
	if content == "" {
		return "Не расслышал, сэр.", false
	}
	// onVoiceCommand has already appended the user's utterance to history.
	history := a.snapshotHistory()
	ctx, cancel := context.WithTimeout(a.voiceContext(), 120*time.Second)
	defer cancel()
	reply, err := a.ollama.Chat(ctx, history)
	if err != nil {
		// Keep the user's utterance in history: onVoiceCommand appends the
		// spoken error as the assistant turn, so removing the user turn here
		// would leave an assistant-only pair and corrupt the conversation.
		message := "Не удалось получить ответ от модели, сэр."
		return message, false
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return "Модель вернула пустой ответ, сэр.", false
	}
	// onVoiceCommand appends the assistant turn after this function returns.
	return reply, false
}

func (a *App) setLastAssistant(text string) {
	a.voiceMu.Lock()
	a.lastAssistant = text
	a.voiceMu.Unlock()
}

func (a *App) getLastAssistant() string {
	a.voiceMu.Lock()
	defer a.voiceMu.Unlock()
	return a.lastAssistant
}

func (a *App) updateVoiceSettings(mutator func(*config.Settings)) error {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	a.stateMu.RLock()
	settings := a.settings
	a.stateMu.RUnlock()
	mutator(&settings)
	settings = config.Normalize(settings)
	if err := validateVoiceSettings(settings); err != nil {
		return err
	}
	if err := config.Save(configPath(), settings); err != nil {
		return err
	}
	a.stateMu.Lock()
	a.settings = settings
	a.stateMu.Unlock()
	return nil
}

func (a *App) handleIncomingVoice(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	a.showWindowSilent()
	cleaned := strings.TrimSpace(sanitize.SanitizeUserInput(command).Content)
	if cleaned != "" {
		a.emitEvent("voice:incoming", map[string]any{"text": cleaned})
		a.emitEvent("voice:transcript", map[string]any{"text": cleaned, "final": true})
	}
	// On a cold start the voice manager may still be loading. Wait briefly so
	// its microphone is paused before the reply is spoken.
	if !a.voiceInputDisabled() {
		deadline := time.Now().Add(3 * time.Second)
		for a.voiceManager() == nil && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
	}
	a.onVoiceCommand(command)
}

func (a *App) handleSecondInstance(args []string) {
	for _, arg := range args {
		switch arg {
		case "--voice-wake":
			a.showWindowSilent()
			go a.pushToTalkWhenReady()
			return
		case "--voice-command-b64":
			// Commands now arrive over authenticated IPC. A legacy command
			// argument is deliberately ignored rather than executed.
			a.showWindow()
			return
		}
	}
	a.showWindow()
}

func (a *App) pushToTalkWhenReady() {
	for attempt := 0; attempt < 150; attempt++ {
		if manager := a.voiceManager(); manager != nil {
			if manager.PushToTalk() {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// GetVoiceStatus returns the current voice configuration and engine state.
func (a *App) GetVoiceStatus() VoiceStatus {
	settings := a.GetSettings()
	manager := a.voiceManager()
	input := voiceinput.Status{State: voiceinput.StateStopped}
	if manager != nil {
		input = manager.Status()
	}
	// Re-resolve on every status request so installing the voice engine while
	// the app is open is reflected without a restart.
	paths := voiceengine.Resolve()
	engine := paths.Status()
	if err := a.verifyVoiceEngine(paths); err != nil {
		engine.IntegrityError = err.Error()
	}
	return VoiceStatus{
		Enabled:  !settings.VoiceInputDisabled,
		State:    string(input.State),
		WakeWord: settings.VoiceWakeWord,
		City:     settings.VoiceCity,
		Engine:   engine,
		Input:    input,
	}
}

// GetVoiceDevices enumerates microphone endpoints.
func (a *App) GetVoiceDevices() ([]voiceinput.Device, error) {
	return voiceinput.ListDevices()
}

// GetMicrophoneDiagnostics checks whether a microphone is connected, allowed by
// Windows privacy settings, and actually picking up sound.
func (a *App) GetMicrophoneDiagnostics() voiceinput.MicrophoneDiagnostics {
	settings := a.GetSettings()
	return voiceinput.DiagnoseMicrophone(settings.VoiceDeviceID)
}

// StartVoiceInput enables and starts continuous listening.
func (a *App) StartVoiceInput() error {
	wasDisabled := a.voiceInputDisabled()
	if err := a.updateVoiceSettings(func(s *config.Settings) { s.VoiceInputDisabled = false }); err != nil {
		return err
	}
	a.syncAutoStart()
	a.interruptVoicePlayback()
	a.stopVoiceInput()
	err := a.startVoiceInput()
	if err != nil && !errors.Is(err, errVoiceStartCanceled) {
		if wasDisabled {
			// The toggle did not take effect; keep persisted settings in sync
			// with the actual state instead of silently claiming success.
			_ = a.updateVoiceSettings(func(s *config.Settings) { s.VoiceInputDisabled = true })
			a.syncAutoStart()
		}
		return err
	}
	return nil
}

// StopVoiceInput disables continuous listening.
func (a *App) StopVoiceInput() error {
	if err := a.updateVoiceSettings(func(s *config.Settings) { s.VoiceInputDisabled = true }); err != nil {
		return err
	}
	a.syncAutoStart()
	a.interruptVoicePlayback()
	a.stopVoiceInput()
	return nil
}

// PushToTalk starts a one-shot command recording.
func (a *App) PushToTalk() bool {
	if manager := a.voiceManager(); manager != nil {
		return manager.PushToTalk()
	}
	return false
}

// SetVoiceDevice selects a microphone and restarts the listener.
func (a *App) SetVoiceDevice(id, name string) error {
	previous := a.GetSettings()
	if err := a.updateVoiceSettings(func(s *config.Settings) {
		s.VoiceDeviceID = strings.TrimSpace(id)
		s.VoiceDeviceName = strings.TrimSpace(name)
	}); err != nil {
		return err
	}
	if err := a.restartVoiceInput(); err != nil {
		rollbackErr := a.updateVoiceSettings(func(s *config.Settings) {
			s.VoiceDeviceID = previous.VoiceDeviceID
			s.VoiceDeviceName = previous.VoiceDeviceName
		})
		if rollbackErr == nil && !a.voiceInputDisabled() {
			_ = a.startVoiceInput()
		}
		if rollbackErr != nil {
			return fmt.Errorf("%w (could not restore the previous microphone: %v)", err, rollbackErr)
		}
		return err
	}
	return nil
}

// DetectWeatherLocation resolves an approximate city from the public IP.
func (a *App) DetectWeatherLocation() (string, error) {
	ctx, cancel := context.WithTimeout(a.voiceBaseContext(), 20*time.Second)
	defer cancel()
	location, err := a.weather.LocateIP(ctx)
	if err != nil {
		return "", err
	}
	if err := a.updateVoiceSettings(func(s *config.Settings) {
		s.VoiceCity = location.Name
		s.VoiceLatitude = location.Latitude
		s.VoiceLongitude = location.Longitude
		s.VoiceLocationAsked = true
	}); err != nil {
		return "", err
	}
	return location.Name, nil
}

// DismissVoiceLocation records that the user declined the one-time location
// prompt. Weather can still resolve the city from the IP on demand.
func (a *App) DismissVoiceLocation() error {
	return a.updateVoiceSettings(func(s *config.Settings) {
		s.VoiceLocationAsked = true
	})
}

// SetWeatherCity validates and stores a city for forecasts.
func (a *App) SetWeatherCity(city string) (string, error) {
	ctx, cancel := context.WithTimeout(a.voiceBaseContext(), 20*time.Second)
	defer cancel()
	location, err := a.weather.Geocode(ctx, city)
	if err != nil {
		return "", err
	}
	if err := a.updateVoiceSettings(func(s *config.Settings) {
		s.VoiceCity = location.Name
		s.VoiceLatitude = location.Latitude
		s.VoiceLongitude = location.Longitude
		s.VoiceLocationAsked = true
	}); err != nil {
		return "", err
	}
	return location.Name, nil
}

func russianDate(now time.Time) string {
	months := []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
	weekdays := []string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}
	return fmt.Sprintf("%s, %d %s %d года", weekdays[int(now.Weekday())], now.Day(), months[int(now.Month())-1], now.Year())
}

var voiceSiteURLs = map[string]string{
	"ютуб":     "https://www.youtube.com",
	"youtube":  "https://www.youtube.com",
	"вк":       "https://vk.com",
	"vk":       "https://vk.com",
	"гугл":     "https://www.google.com",
	"google":   "https://www.google.com",
	"яндекс":   "https://ya.ru",
	"yandex":   "https://ya.ru",
	"github":   "https://github.com",
	"телеграм": "https://web.telegram.org",
	"telegram": "https://web.telegram.org",
	"почта":    "https://mail.ru",
}
