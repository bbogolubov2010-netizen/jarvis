package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"Jarvis/internal/appscan"
	"Jarvis/internal/audio"
)

// RegisterSystemCommands registers OS-related commands.
func RegisterSystemCommands(m *Manager) {
	m.Register(Command{
		Name:        "screenshot",
		Aliases:     []string{"screen", "capture", "shot"},
		Description: "takes a screenshot after confirmation",
		Handler: func(args string) string {
			return ResultScreenshotConfirm
		},
	})
	m.Register(Command{
		Name:        "system",
		Aliases:     []string{"info", "status", "sysinfo"},
		Description: "shows basic system information",
		Handler:     systemInfo,
	})
	m.Register(Command{
		Name:        "open",
		Aliases:     []string{"launch", "start"},
		Description: "opens a known application after confirmation",
		Handler:     openApp,
	})
	m.Register(Command{
		Name:        "apps",
		Aliases:     []string{"applications", "programs"},
		Description: "lists scanned applications, optionally filtered: /apps <query>",
		Handler:     listApps,
	})
	m.Register(Command{
		Name:        "volume",
		Aliases:     []string{"vol", "sound"},
		Description: "sets volume: /volume <0-100> | mute | unmute",
		Handler:     volumeControl,
	})
	m.Register(Command{
		Name:        "shutdown",
		Aliases:     []string{"poweroff", "off"},
		Description: "shuts down Windows after confirmation",
		Handler: func(args string) string {
			return ResultShutdownConfirm
		},
	})
	m.Register(Command{
		Name:        "restart",
		Aliases:     []string{"reboot"},
		Description: "restarts Windows after confirmation",
		Handler: func(args string) string {
			return ResultRestartConfirm
		},
	})
}

func systemInfo(args string) string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return fmt.Sprintf("System Information:\n  Host: %s\n  OS: %s\n  Arch: %s",
		host, runtime.GOOS, runtime.GOARCH)
}

var builtInApps = map[string]struct {
	path string
	args []string
}{
	"notepad":  {"notepad.exe", nil},
	"calc":     {"calc.exe", nil},
	"cmd":      {"cmd.exe", nil},
	"explorer": {systemRootPath("explorer.exe"), nil},
	"paint":    {"mspaint.exe", nil},
}

// appAliases maps Russian and common English nicknames to the stable icon or
// built-in name used by the launcher. Voice input is Russian-first, while the
// detected application list uses English display names.
var appAliases = map[string]string{
	"проводник":         "explorer",
	"файловый менеджер": "explorer",
	"мой компьютер":     "explorer",
	"блокнот":           "notepad",
	"калькулятор":       "calc",
	"командная строка":  "cmd",
	"консоль":           "cmd",
	"паинт":             "paint",
	"пейнт":             "paint",
	"настройки":         "settings",
	"параметры":         "settings",
	"хром":              "chrome",
	"гугл хром":         "chrome",
	"яндекс":            "yandex",
	"яндекс браузер":    "yandex",
	"фаерфокс":          "firefox",
	"телеграм":          "telegram",
	"телеграмм":         "telegram",
	"дискорд":           "discord",
	"спотифай":          "spotify",
	"терминал":          "terminal",
	"повершелл":         "powershell",
}

func canonicalAppName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.Trim(name, " .,!?;:")
	if canonical, ok := appAliases[name]; ok {
		return canonical
	}
	return name
}

func systemRootPath(name string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, name)
}

func system32Path(name string) string {
	return systemRootPath(filepath.Join("System32", name))
}

func openApp(args string) string {
	name := canonicalAppName(args)
	if name == "" {
		return "Usage: /open <appname> - opens an application."
	}
	if name == "settings" {
		command := exec.Command(system32Path("cmd.exe"), "/c", "start", "", "ms-settings:")
		if err := command.Start(); err != nil {
			return fmt.Sprintf("Failed to open Settings: %s", err)
		}
		if command.Process != nil {
			_ = command.Process.Release()
		}
		return "Opened: settings"
	}
	if app, ok := appscan.Lookup(name); ok {
		if err := appscan.Launch(app); err != nil {
			return fmt.Sprintf("Failed to open %s: %s", app.Name, err)
		}
		return fmt.Sprintf("Opened: %s", app.Name)
	}
	app, ok := builtInApps[name]
	if !ok {
		if suggestions := appscan.Suggestions(appscan.Cached().Apps, name, 5); len(suggestions) > 0 {
			return fmt.Sprintf("Unknown application: %s. Did you mean: %s?", args, strings.Join(suggestions, ", "))
		}
		return fmt.Sprintf("Unknown application: %s. Scan the application catalog in Settings, then try again.", args)
	}
	path := app.path
	if !filepath.IsAbs(path) {
		path = system32Path(path)
	}
	command := exec.Command(path, app.args...)
	if err := command.Start(); err != nil {
		return fmt.Sprintf("Failed to open %s: %s", name, err)
	}
	if command.Process != nil {
		_ = command.Process.Release()
	}
	return fmt.Sprintf("Opened: %s", name)
}

// LaunchByName launches any catalogued application by its display name,
// falling back to the built-in allowlist.
func LaunchByName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Usage: /open <appname> - opens an application."
	}
	canonical := canonicalAppName(name)
	if app, ok := appscan.Lookup(name); ok {
		if err := appscan.Launch(app); err != nil {
			return fmt.Sprintf("Failed to launch %s: %s", app.Name, err)
		}
		return fmt.Sprintf("Launched %s", app.Name)
	}
	if canonical != name {
		if app, ok := appscan.Lookup(canonical); ok {
			if err := appscan.Launch(app); err != nil {
				return fmt.Sprintf("Failed to launch %s: %s", app.Name, err)
			}
			return fmt.Sprintf("Launched %s", app.Name)
		}
	}
	for _, app := range DetectApps() {
		if strings.EqualFold(app.Name, name) || strings.EqualFold(app.Icon, name) ||
			strings.EqualFold(app.Icon, canonical) || strings.EqualFold(app.Name, canonical) {
			return LaunchApp(app)
		}
	}
	return openApp(canonical)
}

func listApps(args string) string {
	catalog := appscan.Cached()
	query := strings.ToLower(strings.TrimSpace(args))
	names := make([]string, 0, 40)
	total := 0
	for _, app := range catalog.Apps {
		if query != "" && !strings.Contains(strings.ToLower(app.Name), query) {
			continue
		}
		total++
		if len(names) < 40 {
			names = append(names, app.Name)
		}
	}
	if total == 0 {
		if catalog.ScannedAt.IsZero() {
			return "The application catalog is empty. Open Settings and press \"Scan all applications\"."
		}
		return "No applications matched."
	}
	result := fmt.Sprintf("Applications (%d):\n  %s", total, strings.Join(names, "\n  "))
	if total > len(names) {
		result += fmt.Sprintf("\n  ... and %d more. Use /apps <query> to filter.", total-len(names))
	}
	return result
}

func volumeControl(args string) string {
	value := strings.ToLower(strings.TrimSpace(args))
	if value == "" {
		return "Usage: /volume <0-100> or mute/unmute"
	}

	switch value {
	case "mute":
		if err := audio.SetMuted(true); err != nil {
			return fmt.Sprintf("Mute failed: %s", err)
		}
		return "Audio muted."
	case "unmute":
		if err := audio.SetMuted(false); err != nil {
			return fmt.Sprintf("Unmute failed: %s", err)
		}
		return "Audio unmuted."
	}

	level, err := strconv.Atoi(value)
	if err != nil || level < 0 || level > 100 {
		return "Invalid volume. Use 0-100, mute, or unmute."
	}
	if err := audio.SetVolumePercent(level); err != nil {
		return fmt.Sprintf("Volume change failed: %s", err)
	}
	return fmt.Sprintf("Volume set to %d.", level)
}
