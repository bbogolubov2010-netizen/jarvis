//go:build windows

package appscan

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

// maxScanDepth bounds the Program Files walk. Real applications keep their
// executable within a few levels of the install root.
const maxScanDepth = 4

// maxScannedDirs bounds worst-case scan time on very large installations.
const maxScannedDirs = 40000

// scanTimeout bounds the whole filesystem walk.
const scanTimeout = 25 * time.Second

// trailingVersion matches a display-name suffix such as " 5.0.17".
var trailingVersion = regexp.MustCompile(`(?i)\s+v?\d+(?:\.\d+)+(?:\s.*)?$`)

func execCommand(path string, args ...string) *exec.Cmd {
	command := exec.Command(path, args...)
	command.Dir = filepath.Dir(path)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command
}

func launchAUMID(aumid string) error {
	if strings.ContainsAny(aumid, `"'`+"\r\n") {
		return fmt.Errorf("invalid application id")
	}
	explorer := filepath.Join(os.Getenv("SystemRoot"), "explorer.exe")
	if _, err := os.Stat(explorer); err != nil {
		explorer = "explorer.exe"
	}
	command := exec.Command(explorer, "shell:AppsFolder\\"+aumid)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		return err
	}
	if command.Process != nil {
		_ = command.Process.Release()
	}
	return nil
}

type startMenuEntry struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Args   string `json:"args"`
}

type startAppEntry struct {
	Name  string `json:"name"`
	AppID string `json:"appid"`
}

type shellScan struct {
	Shortcuts []startMenuEntry `json:"shortcuts"`
	StartApps []startAppEntry  `json:"startapps"`
}

// scanStartMenu resolves Start Menu shortcuts and registered app IDs with one
// PowerShell call. Shortcuts give friendly names and real targets; app IDs
// cover UWP/Store applications that have no executable path.
func scanStartMenu(ctx context.Context) []App {
	script := `
$ErrorActionPreference = 'SilentlyContinue'
$out = [ordered]@{ shortcuts = @(); startapps = @() }
$dirs = @(
  (Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs'),
  (Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs')
)
$shell = New-Object -ComObject WScript.Shell
$links = @()
foreach ($dir in $dirs) {
  if (Test-Path -LiteralPath $dir) {
    $links += Get-ChildItem -LiteralPath $dir -Filter *.lnk -Recurse -File
  }
}
foreach ($link in $links) {
  $shortcut = $shell.CreateShortcut($link.FullName)
  if ($shortcut.TargetPath) {
    $out.shortcuts += [pscustomobject]@{
      name = $link.BaseName
      target = $shortcut.TargetPath
      args = [string]$shortcut.Arguments
    }
  }
}
$out.startapps = @(Get-StartApps | Select-Object @{n='name';e={$_.Name}}, @{n='appid';e={$_.AppID}})
$out | ConvertTo-Json -Depth 4 -Compress
`
	command := exec.CommandContext(ctx, powershellPath(), "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.Output()
	if err != nil {
		return nil
	}
	var scanned shellScan
	if err := json.Unmarshal(output, &scanned); err != nil {
		return nil
	}

	apps := make([]App, 0, len(scanned.Shortcuts)+len(scanned.StartApps))
	for _, entry := range scanned.Shortcuts {
		target := strings.TrimSpace(entry.Target)
		if !isExecutable(target) {
			// Shortcuts may point at uninstallers or documents; ignore those.
			continue
		}
		if isHelperExecutable(target) {
			continue
		}
		app := App{
			Name:   cleanName(entry.Name),
			Path:   target,
			Source: "start-menu",
		}
		if args := splitArgs(entry.Args); len(args) > 0 {
			app.Args = args
		}
		apps = append(apps, app)
	}
	for _, entry := range scanned.StartApps {
		name := cleanName(entry.Name)
		id := strings.TrimSpace(entry.AppID)
		if name == "" || id == "" {
			continue
		}
		apps = append(apps, App{Name: name, AUMID: id, Source: "start-apps"})
	}
	return apps
}

func powershellPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	path := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return "powershell.exe"
}

// scanAppPaths reads the App Paths registry keys, which many installers use to
// register the canonical executable for a program name.
func scanAppPaths() []App {
	const appPaths = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`
	type view struct {
		hive   registry.Key
		access uint32
	}
	views := []view{
		{registry.LOCAL_MACHINE, registry.READ | registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, registry.READ | registry.WOW64_32KEY},
		{registry.CURRENT_USER, registry.READ | registry.WOW64_64KEY},
	}
	var apps []App
	for _, v := range views {
		key, err := registry.OpenKey(v.hive, appPaths, v.access)
		if err != nil {
			continue
		}
		names, err := key.ReadSubKeyNames(-1)
		key.Close()
		if err != nil {
			continue
		}
		for _, name := range names {
			sub, err := registry.OpenKey(v.hive, appPaths+`\`+name, v.access)
			if err != nil {
				continue
			}
			value, _, err := sub.GetStringValue("")
			sub.Close()
			if err != nil || !isExecutable(value) || isHelperExecutable(value) {
				continue
			}
			apps = append(apps, App{
				Name:   cleanName(strings.TrimSuffix(name, ".exe")),
				Path:   value,
				Source: "app-paths",
			})
		}
	}
	return apps
}

// exeCandidate is one executable found during the Program Files walk.
type exeCandidate struct {
	path string
	size int64
}

// scanProgramFiles walks the usual install roots and picks one executable per
// application directory. It is intentionally bounded so a scan cannot stall the
// assistant on a huge disk.
func scanProgramFiles(ctx context.Context) []App {
	roots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"),
	}
	deadline := time.Now().Add(scanTimeout)
	var apps []App
	seenDirs := make(map[string]bool)
	visited := 0

	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		if _, err := os.Stat(root); err != nil {
			continue
		}
		dirs := make(map[string][]exeCandidate)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if entry != nil && entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if ctx.Err() != nil || time.Now().After(deadline) || visited > maxScannedDirs {
				return fs.SkipAll
			}
			if entry.IsDir() {
				visited++
				if path != root && (shouldSkipDir(entry.Name()) || isVersionDir(entry.Name())) {
					return fs.SkipDir
				}
				relative, relErr := filepath.Rel(root, path)
				if relErr == nil && strings.Count(relative, string(os.PathSeparator)) >= maxScanDepth {
					return fs.SkipDir
				}
				return nil
			}
			if !isExecutable(path) || isHelperExecutable(path) {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil || info.Size() < 24*1024 {
				return nil
			}
			dir := filepath.Dir(path)
			dirs[dir] = append(dirs[dir], exeCandidate{path: path, size: info.Size()})
			return nil
		})
		if err != nil {
			continue
		}

		for dir, candidates := range dirs {
			key := strings.ToLower(dir)
			if seenDirs[key] {
				continue
			}
			seenDirs[key] = true
			best := pickExecutable(dir, candidates)
			if best == "" {
				continue
			}
			apps = append(apps, App{
				Name:   cleanName(filepath.Base(dir)),
				Path:   best,
				Source: "program-files",
			})
		}
	}
	return apps
}

func pickExecutable(dir string, candidates []exeCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	folder := strings.ToLower(normalize(filepath.Base(dir)))
	best := ""
	bestScore := int64(-1)
	for _, candidate := range candidates {
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(candidate.path), filepath.Ext(candidate.path)))
		score := candidate.size
		if folder != "" && strings.Contains(normalize(base), folder) {
			score += 1 << 40
		}
		if score > bestScore {
			bestScore = score
			best = candidate.path
		}
	}
	return best
}

func shouldSkipDir(name string) bool {
	switch strings.ToLower(name) {
	case "windowsapps", "installer", "temp", "tmp", "cache", "caches", "logs", "log",
		"node_modules", ".git", "uninstall", "unins000", "redist", "redistributable",
		"resources", "locales", "plugins", "themes", "docs", "documentation", "samples",
		"help", "licenses", "license", "crashpad", "crashreports", "swiftshader",
		"updater", "update", "setup":
		return true
	}
	return false
}

func isExecutable(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".exe")
}

// isHelperExecutable filters binaries that should never be presented as an
// application: installers, updaters, crash handlers, redistributables, and
// background services.
func isHelperExecutable(path string) bool {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	for _, token := range []string{
		"unins", "uninstall", "setup", "install", "update", "updater", "upgrade",
		"crash", "report", "helper", "service", "notification", "elevate",
		"vc_redist", "vcredist", "dxsetup", "dxwebsetup", "dotnet", "directx",
		"ffmpeg", "ffplay", "ffprobe", "node", "npm", "npx", "python", "pythonw",
		"java", "javaw", "webview2", "msedgewebview2", "register", "repair",
		"diagnostic", "telemetry", "benchmark", "bench", "test", "demo",
	} {
		if base == token || strings.HasPrefix(base, token) || strings.HasSuffix(base, token) {
			return true
		}
	}
	return false
}

// cleanName removes installer noise, version suffixes, and architecture
// markers from a display name.
func cleanName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".exe")
	for _, suffix := range []string{
		" (x64)", " (x86)", " (64-bit)", " (32-bit)", " (64 bit)", " (32 bit)",
	} {
		name = strings.TrimSuffix(name, suffix)
	}
	if match := trailingVersion.FindStringIndex(name); match != nil && match[0] > 0 {
		name = strings.TrimSpace(name[:match[0]])
	}
	return strings.TrimSpace(name)
}

// splitArgs parses a shortcut argument string into fields. Shortcut arguments
// are rarely quoted in a way that matters here; an empty value returns nil.
func splitArgs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}
