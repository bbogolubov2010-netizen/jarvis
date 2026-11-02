package commands

import (
	"fmt"
	"strings"
	"time"

	"Jarvis/internal/audit"
)

// Sentinel results returned by handlers. They are decoded into a typed Result
// by Process, so the App layer never has to match user-visible text.
const (
	ResultClearChat         = "CMD_CLEAR_CHAT"
	ResultScreenshotConfirm = "CMD_SCREENSHOT_CONFIRM"
	ResultShutdownConfirm   = "CMD_SHUTDOWN_CONFIRM"
	ResultRestartConfirm    = "CMD_RESTART_CONFIRM"
	ResultImageSavedPrefix  = "IMAGE_SAVED:"
	ResultRemindPrefix      = "CMD_REMIND:"
	ResultTimerPrefix       = "CMD_TIMER:"
	ResultListReminders     = "CMD_LIST_REMINDERS"
	ResultGamePrefix        = "CMD_GAME:"
)

// Kind identifies a command outcome that requires app-level handling instead of
// being shown as plain text.
type Kind string

const (
	KindText              Kind = ""
	KindClearChat         Kind = "clear_chat"
	KindScreenshotConfirm Kind = "screenshot_confirm"
	KindShutdownConfirm   Kind = "shutdown_confirm"
	KindRestartConfirm    Kind = "restart_confirm"
	KindImageSaved        Kind = "image_saved"
	KindRemind            Kind = "remind"
	KindTimer             Kind = "timer"
	KindListReminders     Kind = "list_reminders"
	KindGame              Kind = "game"
)

// Result is the decoded outcome of a command. Text holds the reply for
// KindText and the argument payload for the other kinds.
type Result struct {
	Kind Kind
	Text string
}

// decodeResult converts the handler's wire-format string into a Result. The
// encoding is private to this package; callers only see typed values.
func decodeResult(raw string) Result {
	switch raw {
	case ResultClearChat:
		return Result{Kind: KindClearChat}
	case ResultScreenshotConfirm:
		return Result{Kind: KindScreenshotConfirm}
	case ResultShutdownConfirm:
		return Result{Kind: KindShutdownConfirm}
	case ResultRestartConfirm:
		return Result{Kind: KindRestartConfirm}
	case ResultListReminders:
		return Result{Kind: KindListReminders}
	}
	for _, candidate := range []struct {
		prefix string
		kind   Kind
	}{
		{ResultImageSavedPrefix, KindImageSaved},
		{ResultRemindPrefix, KindRemind},
		{ResultTimerPrefix, KindTimer},
		{ResultGamePrefix, KindGame},
	} {
		if strings.HasPrefix(raw, candidate.prefix) {
			return Result{Kind: candidate.kind, Text: strings.TrimPrefix(raw, candidate.prefix)}
		}
	}
	return Result{Kind: KindText, Text: raw}
}

// Command is a single registered command.
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Handler     func(args string) string
}

// Manager routes slash commands to their handlers.
type Manager struct {
	commands []Command
}

// NewManager registers every built-in command.
func NewManager() *Manager {
	m := &Manager{}
	m.Register(Command{
		Name:        "help",
		Aliases:     []string{"commands", "?"},
		Description: "shows this list",
		Handler: func(args string) string {
			return m.Help()
		},
	})
	m.Register(Command{
		Name:        "time",
		Aliases:     []string{"date"},
		Description: "shows the current date and time",
		Handler: func(args string) string {
			return fmt.Sprintf("Current time: %s", time.Now().Format("Mon Jan 2 15:04:05 2006"))
		},
	})
	m.Register(Command{
		Name:        "clear",
		Aliases:     []string{"clean"},
		Description: "clears the chat history",
		Handler: func(args string) string {
			return ResultClearChat
		},
	})
	m.Register(Command{
		Name:        "audit",
		Aliases:     []string{"logs"},
		Description: "shows recent audit log entries",
		Handler: func(args string) string {
			return audit.GetLogs()
		},
	})
	m.Register(Command{
		Name:        "remind",
		Aliases:     []string{"reminder"},
		Description: "sets a reminder: /remind <minutes> <text>",
		Handler: func(args string) string {
			return ResultRemindPrefix + args
		},
	})
	m.Register(Command{
		Name:        "timer",
		Description: "sets a timer: /timer <minutes>",
		Handler: func(args string) string {
			return ResultTimerPrefix + args
		},
	})
	m.Register(Command{
		Name:        "reminders",
		Aliases:     []string{"remindlist"},
		Description: "lists pending reminders",
		Handler: func(args string) string {
			return ResultListReminders
		},
	})
	m.Register(Command{
		Name:        "game",
		Aliases:     []string{"games"},
		Description: "shows the current game: /game [status|list|add|forget <name>]",
		Handler: func(args string) string {
			return ResultGamePrefix + args
		},
	})

	RegisterWebCommands(m)
	RegisterImageCommands(m)
	RegisterSystemCommands(m)
	RegisterNotesCommands(m)
	RegisterFileCommands(m)

	return m
}

// Register adds a command. Duplicate triggers are prevented by tests.
func (m *Manager) Register(cmd Command) {
	m.commands = append(m.commands, cmd)
}

// Help renders the command list.
func (m *Manager) Help() string {
	var out strings.Builder
	out.WriteString("Available commands:\n\n")
	for _, cmd := range m.commands {
		out.WriteString(fmt.Sprintf("  /%s", cmd.Name))
		if len(cmd.Aliases) > 0 {
			out.WriteString(fmt.Sprintf(" (%s)", strings.Join(cmd.Aliases, ", ")))
		}
		out.WriteString(fmt.Sprintf(" - %s\n", cmd.Description))
	}
	out.WriteString("\nType /<command> to use a command, or just describe what you need.")
	return out.String()
}

// Process executes the first command matching text and returns its typed
// outcome.
func (m *Manager) Process(text string) (Result, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Result{}, false
	}
	lower := strings.ToLower(trimmed)
	for _, cmd := range m.commands {
		if result, ok := matchCmd(lower, trimmed, cmd.Name, cmd.Handler); ok {
			return decodeResult(result), true
		}
		for _, alias := range cmd.Aliases {
			if result, ok := matchCmd(lower, trimmed, alias, cmd.Handler); ok {
				return decodeResult(result), true
			}
		}
	}
	return Result{}, false
}

func matchCmd(lower, original, name string, handler func(string) string) (string, bool) {
	prefix := "/" + name
	if lower == prefix {
		return handler(""), true
	}
	if strings.HasPrefix(lower, prefix+" ") {
		return handler(strings.TrimSpace(original[len(prefix):])), true
	}
	return "", false
}
