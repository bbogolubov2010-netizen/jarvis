package main

import (
	"fmt"
	"strings"
	"time"

	"Jarvis/internal/gamewatch"
)

// gameWatchDisabled reports whether game detection is turned off in Settings.
func (a *App) gameWatchDisabled() bool {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.settings.GameWatchDisabled
}

func (a *App) gameBreakDisabled() bool {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.settings.GameBreakRemindersDisabled
}

// pollGame samples the foreground window, updates playtime and emits proactive
// messages for session starts, break prompts and unknown games.
func (a *App) pollGame(now time.Time) {
	if a.game == nil || a.gameWatchDisabled() {
		return
	}

	status := a.game.Observe(now)
	a.gameMu.Lock()
	previous := a.gameStatus
	a.gameStatus = status
	a.gameMu.Unlock()

	if status.Playing && !status.Candidate && (!previous.Playing || !strings.EqualFold(previous.Exe, status.Exe)) {
		if a.autonomyEnabled() && !a.inQuietHours() {
			a.emitProactive(fmt.Sprintf(
				"Сэр, вижу %s. Сессия пошла — %s. Если понадобится таймер на перерыв, скажите.",
				status.Name, humanDuration(status.SessionSeconds)), "game")
		}
	}

	if status.Candidate && !a.gameAsked[status.Exe] {
		a.gameAsked[status.Exe] = true
		if a.autonomyEnabled() && !a.inQuietHours() {
			a.emitProactive(fmt.Sprintf(
				"Сэр, не узнаю %s. Это игра? Скажите «запомни игру», чтобы я учитывал её сессии.",
				status.Name), "game")
		}
	}

	if status.BreakDue && !a.gameBreakDisabled() && a.autonomyEnabled() && !a.inQuietHours() {
		a.game.MarkBreakPrompted(now)
		a.emitProactive(fmt.Sprintf(
			"Сэр, вы играете в %s уже %s. Предлагаю пятиминутный перерыв.",
			status.Name, humanDuration(status.SessionSeconds)), "game_break")
	}
}

// currentGameSummary is used by the startup briefing.
func (a *App) currentGameSummary() string {
	if a.game == nil || a.gameWatchDisabled() {
		return ""
	}
	status := a.game.Status()
	if !status.Playing {
		return ""
	}
	if status.SessionSeconds < 60 {
		return fmt.Sprintf("Вижу, вы только что запустили %s.", status.Name)
	}
	return fmt.Sprintf("Вижу, вы играете в %s уже %s.", status.Name, humanDuration(status.SessionSeconds))
}

// GetGameStatus returns the current session for the UI and /game command.
func (a *App) GetGameStatus() gamewatch.Status {
	if a.game == nil {
		return gamewatch.Status{}
	}
	status := a.game.Status()
	a.gameMu.Lock()
	a.gameStatus = status
	a.gameMu.Unlock()
	return status
}

func (a *App) handleGameCommand(args string) string {
	if a.game == nil {
		return "Game tracking is unavailable, Sir."
	}
	lower := strings.ToLower(strings.TrimSpace(args))

	switch {
	case lower == "" || lower == "status":
		status := a.GetGameStatus()
		if !status.Playing {
			return "Сейчас игра не запущена, сэр. Скажите «запомни игру», когда запустите новую."
		}
		return fmt.Sprintf("Играю в %s. Сессия %s, сегодня %s.",
			status.Name, humanDuration(status.SessionSeconds), humanDuration(status.TodaySeconds))

	case lower == "list":
		games := a.game.List()
		if len(games) == 0 {
			return "Список игр пуст, сэр."
		}
		var builder strings.Builder
		builder.WriteString("Известные игры:\n")
		for _, game := range games {
			builder.WriteString(fmt.Sprintf("- %s (%s)\n", game.Name, game.Exe))
		}
		return strings.TrimSpace(builder.String())

	case lower == "add" || strings.HasPrefix(lower, "add "):
		name := strings.TrimSpace(args[len("add"):])
		// While the user talks to JARVIS, its own window is in the
		// foreground, so prefer the active game session over the current
		// foreground process.
		game, ok := a.game.RememberSession(name)
		if !ok {
			game, ok = a.game.AddCurrent(name)
		}
		if !ok {
			return "Не вижу активного окна, сэр. Запустите игру и повторите."
		}
		return fmt.Sprintf("Запомнил игру: %s.", game.Name)

	case strings.HasPrefix(lower, "forget "):
		query := strings.TrimSpace(args[len("forget"):])
		if a.game.Forget(query) {
			return fmt.Sprintf("Убрал %s из списка игр.", query)
		}
		return fmt.Sprintf("Не нашёл %s в списке игр.", query)

	default:
		return "Usage: /game [status|list|add [name]|forget <name>]"
	}
}

func (a *App) voiceGameStatus() (string, bool) {
	return a.handleGameCommand("status"), false
}

func (a *App) voiceGameRemember() (string, bool) {
	return a.handleGameCommand("add"), false
}

func humanDuration(seconds int64) string {
	if seconds < 60 {
		return "меньше минуты"
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%d мин", minutes)
	}
	hours := minutes / 60
	rest := minutes % 60
	if rest == 0 {
		return fmt.Sprintf("%d ч", hours)
	}
	return fmt.Sprintf("%d ч %d мин", hours, rest)
}
