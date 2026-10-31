package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"Jarvis/internal/appscan"
)

// InstalledApp is a detected desktop application.
type InstalledApp struct {
	Name string   `json:"name"`
	Icon string   `json:"icon"`
	Path string   `json:"path"`
	Args []string `json:"args"`
}

// DetectApps probes well-known installation paths.
func DetectApps() []InstalledApp {
	var apps []InstalledApp

	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	localAppData := os.Getenv("LOCALAPPDATA")
	systemRoot := os.Getenv("SystemRoot")

	checks := []struct {
		name  string
		icon  string
		paths []string
		args  []string
	}{
		{"Google Chrome", "chrome", []string{
			filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
		}, nil},
		{"Microsoft Edge", "edge", []string{
			filepath.Join(systemRoot, "SystemApps", "Microsoft.MicrosoftEdge_8wekyb3d8bbwe", "MicrosoftEdge.exe"),
			filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
		}, nil},
		{"Mozilla Firefox", "firefox", []string{
			filepath.Join(programFiles, "Mozilla Firefox", "firefox.exe"),
			filepath.Join(programFilesX86, "Mozilla Firefox", "firefox.exe"),
		}, nil},
		{"Brave", "brave", []string{
			filepath.Join(programFiles, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
			filepath.Join(programFilesX86, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
		}, nil},
		{"Opera", "opera", []string{
			filepath.Join(programFiles, "Opera", "launcher.exe"),
			filepath.Join(programFilesX86, "Opera", "launcher.exe"),
		}, nil},
		{"Yandex Browser", "yandex", []string{
			filepath.Join(programFilesX86, "Yandex", "YandexBrowser", "browser.exe"),
			filepath.Join(programFiles, "Yandex", "YandexBrowser", "browser.exe"),
		}, nil},
		{"Notepad", "notepad", []string{
			filepath.Join(systemRoot, "System32", "notepad.exe"),
		}, nil},
		{"Calculator", "calc", []string{
			filepath.Join(systemRoot, "System32", "calc.exe"),
		}, nil},
		{"File Explorer", "explorer", []string{
			filepath.Join(systemRoot, "explorer.exe"),
		}, nil},
		{"Command Prompt", "cmd", []string{
			filepath.Join(systemRoot, "System32", "cmd.exe"),
		}, nil},
		{"PowerShell", "powershell", []string{
			filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
		}, []string{"-NoExit"}},
		{"Terminal", "terminal", []string{
			filepath.Join(localAppData, "Microsoft", "WindowsApps", "wt.exe"),
			filepath.Join(programFiles, "WindowsApps", "Microsoft.WindowsTerminal_*", "wt.exe"),
		}, nil},
		{"VS Code", "vscode", []string{
			filepath.Join(programFiles, "Microsoft VS Code", "Code.exe"),
			filepath.Join(localAppData, "Programs", "Microsoft VS Code", "Code.exe"),
		}, nil},
		{"Git Bash", "gitbash", []string{
			filepath.Join(programFiles, "Git", "git-bash.exe"),
		}, nil},
		{"Process Explorer", "procexp", []string{
			filepath.Join(localAppData, "Sysinternals", "procexp64.exe"),
		}, nil},
		{"Spotify", "spotify", []string{
			filepath.Join(localAppData, "Spotify", "Spotify.exe"),
		}, nil},
		{"Discord", "discord", []string{
			filepath.Join(localAppData, "Discord", "Update.exe"),
		}, []string{"--processStart", "Discord.exe"}},
		{"Telegram", "telegram", []string{
			filepath.Join(localAppData, "Telegram Desktop", "Telegram.exe"),
		}, nil},
		{"Ollama", "ollama", []string{
			filepath.Join(localAppData, "Programs", "Ollama", "ollama.exe"),
			filepath.Join(localAppData, "Ollama", "ollama.exe"),
		}, nil},
	}

	for _, candidate := range checks {
		if path := firstExisting(candidate.paths); path != "" {
			apps = append(apps, InstalledApp{
				Name: candidate.name,
				Icon: candidate.icon,
				Path: path,
				Args: candidate.args,
			})
		}
	}
	return apps
}

func firstExisting(paths []string) string {
	for _, pattern := range paths {
		if pattern == "" || !filepath.IsAbs(pattern) {
			// An unset environment variable must not turn a probe into a
			// relative path under the current working directory.
			continue
		}
		if !strings.Contains(pattern, "*") {
			if info, err := os.Stat(pattern); err == nil && !info.IsDir() {
				return pattern
			}
			continue
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			if info, err := os.Stat(match); err == nil && !info.IsDir() {
				return match
			}
		}
	}
	return ""
}

// CatalogApps returns the scanned application catalog for the sidebar.
func CatalogApps() []InstalledApp {
	catalog := appscan.Cached()
	apps := make([]InstalledApp, 0, len(catalog.Apps))
	for _, app := range catalog.Apps {
		apps = append(apps, InstalledApp{
			Name: app.Name,
			Icon: app.Name,
			Path: app.Path,
			Args: app.Args,
		})
	}
	return apps
}

// LaunchApp starts a detected application.
func LaunchApp(app InstalledApp) string {
	command := exec.Command(app.Path, app.Args...)
	if err := command.Start(); err != nil {
		return fmt.Sprintf("Failed to launch %s: %s", app.Name, err)
	}
	if command.Process != nil {
		_ = command.Process.Release()
	}
	return fmt.Sprintf("Launched %s", app.Name)
}
