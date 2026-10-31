package commands

import (
	"fmt"
	"strings"
	"time"

	"Jarvis/internal/audit"
)

// Sentinel results returned by handlers and interpreted by the App layer.
// They never contain human-readable text, so routing cannot be confused with
// a reply shown to the user.
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

// Process executes the first command matching text.
func (m *Manager) Process(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	for _, cmd := range m.commands {
		if result, ok := matchCmd(lower, trimmed, cmd.Name, cmd.Handler); ok {
			return result, true
		}
		for _, alias := range cmd.Aliases {
			if result, ok := matchCmd(lower, trimmed, alias, cmd.Handler); ok {
				return result, true
			}
		}
	}
	return "", false
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
