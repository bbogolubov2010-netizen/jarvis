package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"Jarvis/internal/intent"
	"Jarvis/internal/ollama"
	"Jarvis/internal/voice"
)

const eventProactiveMessage = "proactive:message"

// autonomyLevel returns the persisted autonomy setting.
func (a *App) autonomyLevel() string {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.settings.AutonomyLevel
}

func (a *App) autonomyEnabled() bool {
	return a.autonomyLevel() != "off"
}

// inQuietHours keeps unprompted messages out of the late night. The window is
// deliberately fixed: proactive features must never wake the user.
func (a *App) inQuietHours() bool {
	hour := time.Now().Hour()
	return hour >= 23 || hour < 8
}

// scheduleBriefing queues the startup briefing. A hidden logon launch keeps it
// pending until the user actually opens the window.
func (a *App) scheduleBriefing() {
	settings := a.GetSettings()
	if settings.ProactiveBriefingDisabled || !a.autonomyEnabled() {
		return
	}
	a.proactiveMu.Lock()
	a.briefingPending = true
	a.proactiveMu.Unlock()

	go func() {
		timer := time.NewTimer(6 * time.Second)
		defer timer.Stop()
		select {
		case <-a.voiceCtx.Done():
			return
		case <-timer.C:
		}
		a.deliverBriefingIfVisible()
	}()
}

func (a *App) deliverBriefingIfVisible() {
	if !a.windowIsVisible() || a.inQuietHours() {
		return
	}
	a.proactiveMu.Lock()
	if !a.briefingPending || a.briefingDelivered {
		a.proactiveMu.Unlock()
		return
	}
	a.briefingPending = false
	a.briefingDelivered = true
	a.proactiveMu.Unlock()

	a.emitProactive(a.briefingText(), "briefing")
}

func (a *App) briefingText() string {
	settings := a.GetSettings()
	now := time.Now()
	greeting := "доброй ночи"
	switch {
	case now.Hour() >= 5 && now.Hour() < 12:
		greeting = "доброе утро"
	case now.Hour() >= 12 && now.Hour() < 18:
		greeting = "добрый день"
	case now.Hour() >= 18:
		greeting = "добрый вечер"
	}

	parts := []string{fmt.Sprintf("Сэр, %s. Сейчас %s.", greeting, now.Format("15:04"))}

	if city := strings.TrimSpace(settings.VoiceCity); city != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		forecast, err := a.weather.Get(ctx, city, "now")
		cancel()
		if err == nil {
			parts = append(parts, fmt.Sprintf("В %s сейчас %.0f°C.", city, forecast.Current.Temperature))
		}
	}

	if a.reminders != nil {
		if items := a.reminders.List(); len(items) > 0 {
			next := items[0]
			parts = append(parts, fmt.Sprintf("Ближайшее напоминание: %s в %s.", next.Text, next.Due.Format("15:04")))
		}
	}

	if game := a.currentGameSummary(); game != "" {
		parts = append(parts, game)
	}

	parts = append(parts, "Все системы в норме.")
	return strings.Join(parts, " ")
}

// emitProactive records an unprompted message in history and pushes it to the
// chat UI. It never speaks the full text; only short phrase-pack sounds are used.
func (a *App) emitProactive(content, kind string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	a.proactiveMu.Lock()
	a.lastProactive = time.Now()
	a.proactiveMu.Unlock()

	a.appendHistory(ollama.Message{Role: "assistant", Content: content})
	a.emitEvent(eventProactiveMessage, map[string]any{"content": content, "kind": kind})
}

func (a *App) startProactiveLoop() {
	started := time.Now()
	a.pollGame(started)

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.voiceCtx.Done():
			return
		case now := <-ticker.C:
			a.deliverDueReminders(now)
			a.pollGame(now)
			a.checkWatchdog(now, started)
		}
	}
}

func (a *App) deliverDueReminders(now time.Time) {
	if a.reminders == nil {
		return
	}
	for _, item := range a.reminders.Due(now) {
		a.emitProactive("Сэр, напоминание: "+item.Text+".", "reminder")
		a.playVoice(voice.KeyAck)
	}
}

// checkWatchdog reports a local model outage once per run. It stays silent
// while the window is hidden, during quiet hours, or when proactivity is off.
func (a *App) checkWatchdog(now, started time.Time) {
	if !a.autonomyEnabled() || a.inQuietHours() || !a.windowIsVisible() {
		return
	}
	if now.Sub(started) < 2*time.Minute {
		return
	}
	a.proactiveMu.Lock()
	delivered := a.watchdogDelivered
	a.proactiveMu.Unlock()
	if delivered {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if a.ollama.Healthy(ctx) == nil {
		return
	}

	a.proactiveMu.Lock()
	if a.watchdogDelivered {
		a.proactiveMu.Unlock()
		return
	}
	a.watchdogDelivered = true
	a.proactiveMu.Unlock()
	a.emitProactive("Сэр, локальная модель не отвечает. Проверьте, запущен ли Ollama — до тех пор я могу работать только с командами и голосом.", "health")
}

// addReminder stores a reminder and returns the confirmation shown to the user.
func (a *App) addReminder(text string, due time.Time) string {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "Напоминание"
	}
	if a.reminders == nil {
		return "Напоминания недоступны, сэр."
	}
	item, err := a.reminders.Add(text, due)
	if err != nil {
		return "Не удалось сохранить напоминание, сэр: " + err.Error()
	}
	return fmt.Sprintf("Напоминание установлено на %s: %s.", formatReminderTime(item.Due), item.Text)
}

func (a *App) listReminders() string {
	if a.reminders == nil {
		return "Напоминания недоступны, сэр."
	}
	items := a.reminders.List()
	if len(items) == 0 {
		return "Напоминаний нет, сэр."
	}
	var builder strings.Builder
	builder.WriteString("Ваши напоминания:\n")
	for _, item := range items {
		builder.WriteString(fmt.Sprintf("- %s — %s\n", formatReminderTime(item.Due), item.Text))
	}
	return strings.TrimSpace(builder.String())
}

func formatReminderTime(due time.Time) string {
	if due.Format("2006-01-02") == time.Now().Format("2006-01-02") {
		return due.Format("15:04")
	}
	return due.Format("02.01 15:04")
}

func (a *App) voiceReminder(action intent.VoiceAction) (string, bool) {
	due, ok := reminderDue(action)
	if !ok {
		return "На сколько поставить напоминание, сэр? Скажите, например: «напомни через 10 минут позвонить».", false
	}
	return a.addReminder(action.Arg, due), false
}

func (a *App) voiceListReminders() (string, bool) {
	return a.listReminders(), false
}

func (a *App) handleRemindCommand(args string) string {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return "Usage: /remind <minutes> <text>"
	}
	minutes, err := strconv.Atoi(fields[0])
	if err != nil || minutes <= 0 || minutes > 10080 {
		return "Usage: /remind <minutes> <text> (1 minute to 7 days)"
	}
	text := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
	if text == "" {
		text = "Напоминание"
	}
	return a.addReminder(text, time.Now().Add(time.Duration(minutes)*time.Minute))
}

func (a *App) handleTimerCommand(args string) string {
	minutes, err := strconv.Atoi(strings.TrimSpace(args))
	if err != nil || minutes <= 0 || minutes > 10080 {
		return "Usage: /timer <minutes> (1 minute to 7 days)"
	}
	return a.addReminder("Таймер", time.Now().Add(time.Duration(minutes)*time.Minute))
}

// reminderDue resolves the parsed delay or clock time into an absolute time.
func reminderDue(action intent.VoiceAction) (time.Time, bool) {
	now := time.Now()
	if action.At != "" {
		parsed, err := time.Parse("15:04", action.At)
		if err == nil {
			due := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
			if !due.After(now) {
				due = due.Add(24 * time.Hour)
			}
			return due, true
		}
	}
	if action.Minutes <= 0 {
		return time.Time{}, false
	}
	return now.Add(time.Duration(action.Minutes) * time.Minute), true
}
