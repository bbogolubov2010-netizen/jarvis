// Package gamewatch detects the game in the foreground and tracks playtime.
// It is deliberately read-only: no process injection, no memory reads and no
// input automation, so anti-cheat systems are never touched.
package gamewatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Game is a remembered title.
type Game struct {
	Name   string `json:"name"`
	Exe    string `json:"exe"`
	Path   string `json:"path"`
	Source string `json:"source"`
}

// Status is the current game session snapshot.
type Status struct {
	Playing        bool   `json:"playing"`
	Name           string `json:"name"`
	Exe            string `json:"exe"`
	Source         string `json:"source"`
	StartedAt      int64  `json:"started_at"`
	SessionSeconds int64  `json:"session_seconds"`
	TodaySeconds   int64  `json:"today_seconds"`
	BreakDue       bool   `json:"break_due"`
	Candidate      bool   `json:"candidate"`
}

type sessionState struct {
	game     Game
	started  time.Time
	lastSeen time.Time
}

type fileData struct {
	Games []Game                      `json:"games"`
	Days  map[string]map[string]int64 `json:"days"`
}

// Watcher owns the remembered games and the active session.
type Watcher struct {
	mu        sync.Mutex
	path      string
	games     []Game
	days      map[string]map[string]int64
	session   *sessionState
	lastBreak time.Time
}

const (
	foregroundGrace = 90 * time.Second
	breakInterval   = time.Hour
)

var nonGameProcesses = map[string]bool{
	"explorer.exe": true, "chrome.exe": true, "msedge.exe": true, "firefox.exe": true,
	"brave.exe": true, "opera.exe": true, "vivaldi.exe": true, "yandex.exe": true,
	"code.exe": true, "code - insiders.exe": true, "devenv.exe": true, "idea64.exe": true,
	"pycharm64.exe": true, "webstorm64.exe": true, "goland64.exe": true, "rider64.exe": true,
	"discord.exe": true, "telegram.exe": true, "slack.exe": true, "teams.exe": true,
	"ms-teams.exe": true, "zoom.exe": true, "spotify.exe": true, "obs64.exe": true,
	"jarvis.exe": true, "applicationframehost.exe": true, "searchhost.exe": true,
	"shellexperiencehost.exe": true, "startmenuexperiencehost.exe": true, "lockapp.exe": true,
	"systemsettings.exe": true, "taskmgr.exe": true, "notepad.exe": true, "cmd.exe": true,
	"powershell.exe": true, "windowsterminal.exe": true, "wt.exe": true, "mspaint.exe": true,
	"calculator.exe": true, "calc.exe": true, "winword.exe": true, "excel.exe": true,
	"powerpnt.exe": true, "outlook.exe": true, "thunderbird.exe": true, "steam.exe": true,
	"steamwebhelper.exe": true, "epicgameslauncher.exe": true, "galaxyclient.exe": true,
	"battle.net.exe": true, "origin.exe": true, "eadesktop.exe": true, "ubisoftconnect.exe": true,
	"upc.exe": true, "riotclientservices.exe": true, "vlc.exe": true, "mpc-hc64.exe": true,
}

// New loads remembered games and merges titles recorded by Windows Game DVR.
func New(path string) (*Watcher, error) {
	watcher := &Watcher{
		path: path,
		days: make(map[string]map[string]int64),
	}
	data, err := os.ReadFile(path)
	if err == nil {
		var stored fileData
		if err := json.Unmarshal(data, &stored); err != nil {
			backup := path + ".corrupt-" + time.Now().Format("20060102-150405")
			_ = os.Rename(path, backup)
		} else {
			watcher.games = stored.Games
			if stored.Days != nil {
				watcher.days = stored.Days
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return watcher, fmt.Errorf("read games: %w", err)
	}

	watcher.mergeDVR()
	watcher.pruneDays()
	watcher.sortGames()
	return watcher, nil
}

// mergeDVR imports executable paths Windows recorded for recognized games.
func (w *Watcher) mergeDVR() {
	const root = `System\GameConfigStore\Children`
	key, err := registry.OpenKey(registry.CURRENT_USER, root, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return
	}
	defer key.Close()

	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return
	}
	for _, name := range names {
		child, err := registry.OpenKey(registry.CURRENT_USER, root+`\`+name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		value, _, err := child.GetStringValue("ExecutablePath")
		child.Close()
		if err != nil || strings.TrimSpace(value) == "" {
			continue
		}
		w.addLocked(Game{
			Name:   displayName(value),
			Exe:    strings.ToLower(filepath.Base(value)),
			Path:   value,
			Source: "dvr",
		})
	}
}

// Add remembers a game by executable path.
func (w *Watcher) Add(name, path string) Game {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.TrimSpace(name) == "" {
		name = displayName(path)
	}
	game := Game{
		Name:   strings.TrimSpace(name),
		Exe:    strings.ToLower(filepath.Base(path)),
		Path:   path,
		Source: "manual",
	}
	w.addLocked(game)
	_ = w.saveLocked()
	return game
}

// AddCurrent remembers the process currently in the foreground.
func (w *Watcher) AddCurrent(name string) (Game, bool) {
	info, err := foreground()
	if err != nil || info.Path == "" {
		return Game{}, false
	}
	return w.Add(name, info.Path), true
}

// RememberSession promotes the game in the active session to the manual list.
// This is used by voice and chat commands because the foreground window is
// JARVIS itself while the user is talking to it.
func (w *Watcher) RememberSession(name string) (Game, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.session == nil {
		return Game{}, false
	}
	game := w.session.game
	if strings.TrimSpace(name) != "" {
		game.Name = strings.TrimSpace(name)
	}
	game.Source = "manual"
	w.addLocked(game)
	_ = w.saveLocked()
	return game, true
}

// Forget removes a game by name or executable.
func (w *Watcher) Forget(query string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	query = strings.ToLower(strings.TrimSpace(query))
	for index, game := range w.games {
		if strings.ToLower(game.Name) == query || strings.ToLower(game.Exe) == query {
			w.games = append(w.games[:index], w.games[index+1:]...)
			_ = w.saveLocked()
			return true
		}
	}
	return false
}

// List returns remembered games sorted by name.
func (w *Watcher) List() []Game {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Game(nil), w.games...)
}

// Observe samples the foreground window and updates the session. It must be
// called regularly (every few seconds).
func (w *Watcher) Observe(now time.Time) Status {
	w.mu.Lock()
	defer w.mu.Unlock()

	info, err := foreground()
	if err != nil {
		w.expireLocked(now)
		return w.statusLocked(now)
	}

	game, remembered := w.lookupLocked(info.Exe)
	candidate := false
	if !remembered {
		if !looksLikeGame(info) {
			w.expireLocked(now)
			return w.statusLocked(now)
		}
		game = Game{
			Name:   displayName(info.Path),
			Exe:    info.Exe,
			Path:   info.Path,
			Source: "heuristic",
		}
		candidate = true
	}

	if w.session == nil || w.session.game.Exe != game.Exe {
		w.closeSessionLocked(now)
		w.session = &sessionState{game: game, started: now, lastSeen: now}
	} else {
		// Count only the elapsed interval while the game was actually in the
		// foreground, capped so a suspended machine does not add hours.
		delta := now.Sub(w.session.lastSeen)
		if delta > 0 && delta < 30*time.Second {
			w.addPlaytimeLocked(game.Exe, int64(delta/time.Second))
		}
		w.session.lastSeen = now
	}

	status := w.statusLocked(now)
	status.Candidate = candidate
	return status
}

// MarkBreakPrompted records that the user was offered a break.
func (w *Watcher) MarkBreakPrompted(now time.Time) {
	w.mu.Lock()
	w.lastBreak = now
	w.mu.Unlock()
}

// Status returns the current snapshot without sampling the screen.
func (w *Watcher) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.statusLocked(time.Now())
}

func (w *Watcher) statusLocked(now time.Time) Status {
	if w.session == nil {
		return Status{}
	}
	elapsed := now.Sub(w.session.started)
	if elapsed < 0 {
		elapsed = 0
	}
	return Status{
		Playing:        true,
		Name:           w.session.game.Name,
		Exe:            w.session.game.Exe,
		Source:         w.session.game.Source,
		StartedAt:      w.session.started.UnixMilli(),
		SessionSeconds: int64(elapsed / time.Second),
		TodaySeconds:   w.todayLocked(w.session.game.Exe),
		BreakDue:       elapsed >= breakInterval && (w.lastBreak.IsZero() || now.Sub(w.lastBreak) >= breakInterval),
	}
}

func (w *Watcher) lookupLocked(exe string) (Game, bool) {
	for _, game := range w.games {
		if strings.EqualFold(game.Exe, exe) {
			return game, true
		}
	}
	return Game{}, false
}

func (w *Watcher) addLocked(game Game) {
	if game.Exe == "" {
		return
	}
	for index, existing := range w.games {
		if strings.EqualFold(existing.Exe, game.Exe) {
			// A manual entry is more trustworthy than a heuristic one.
			if game.Source == "manual" {
				w.games[index] = game
			}
			return
		}
	}
	w.games = append(w.games, game)
}

func (w *Watcher) sortGames() {
	w.mu.Lock()
	sort.SliceStable(w.games, func(i, j int) bool {
		return strings.ToLower(w.games[i].Name) < strings.ToLower(w.games[j].Name)
	})
	w.mu.Unlock()
}

func (w *Watcher) expireLocked(now time.Time) {
	if w.session == nil {
		return
	}
	if now.Sub(w.session.lastSeen) < foregroundGrace {
		return
	}
	w.closeSessionLocked(now)
}

func (w *Watcher) closeSessionLocked(now time.Time) {
	if w.session == nil {
		return
	}
	w.session = nil
	w.lastBreak = time.Time{}
	_ = w.saveLocked()
}

func (w *Watcher) addPlaytimeLocked(exe string, seconds int64) {
	if seconds <= 0 {
		return
	}
	day := time.Now().Format("2006-01-02")
	if w.days[day] == nil {
		w.days[day] = make(map[string]int64)
	}
	w.days[day][exe] += seconds
}

func (w *Watcher) todayLocked(exe string) int64 {
	day := time.Now().Format("2006-01-02")
	if w.days[day] == nil {
		return 0
	}
	return w.days[day][exe]
}

func (w *Watcher) pruneDays() {
	w.mu.Lock()
	defer w.mu.Unlock()
	cutoff := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	for day := range w.days {
		if day < cutoff {
			delete(w.days, day)
		}
	}
}

func (w *Watcher) saveLocked() error {
	data, err := json.MarshalIndent(fileData{Games: w.games, Days: w.days}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(w.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".games.tmp-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(name)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, w.path); err != nil {
		return err
	}
	renamed = true
	return nil
}

func displayName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.TrimSpace(name)
	if name == "" {
		return "Unknown game"
	}
	runes := []rune(name)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

// windowInfo is the subset of the foreground window needed for detection.
type windowInfo struct {
	Exe        string
	Path       string
	Title      string
	Fullscreen bool
	HasCaption bool
}

func looksLikeGame(info windowInfo) bool {
	if info.Exe == "" || nonGameProcesses[strings.ToLower(info.Exe)] {
		return false
	}
	if strings.Contains(strings.ToLower(info.Path), `\windows\`) {
		return false
	}
	// Borderless fullscreen or a caption-less window that covers the monitor.
	return info.Fullscreen || !info.HasCaption
}

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	getWindowRect     = user32.NewProc("GetWindowRect")
	getWindowLongW    = user32.NewProc("GetWindowLongW")
	monitorFromWindow = user32.NewProc("MonitorFromWindow")
	getMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
)

type rect struct {
	Left, Top, Right, Bottom int32
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

const (
	gwlStyle                = uintptr(0xFFFFFFF0) // GWL_STYLE (-16)
	wsCaption               = 0x00C00000
	monitorDefaultToNearest = 2
)

func foreground() (windowInfo, error) {
	handle := windows.GetForegroundWindow()
	if handle == 0 {
		return windowInfo{}, errors.New("no foreground window")
	}
	var processID uint32
	windows.GetWindowThreadProcessId(handle, &processID)
	if processID == 0 {
		return windowInfo{}, errors.New("no foreground process")
	}

	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return windowInfo{}, err
	}
	defer windows.CloseHandle(process)

	buffer := make([]uint16, 1024)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return windowInfo{}, err
	}
	path := windows.UTF16ToString(buffer[:size])

	info := windowInfo{
		Exe:   filepath.Base(path),
		Path:  path,
		Title: windowTitle(handle),
	}
	style, _, _ := getWindowLongW.Call(uintptr(handle), uintptr(gwlStyle))
	info.HasCaption = uint32(style)&wsCaption != 0

	var windowRect rect
	if ok, _, _ := getWindowRect.Call(uintptr(handle), uintptr(unsafe.Pointer(&windowRect))); ok != 0 {
		monitor, _, _ := monitorFromWindow.Call(uintptr(handle), monitorDefaultToNearest)
		if monitor != 0 {
			mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
			if ok, _, _ := getMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&mi))); ok != 0 {
				info.Fullscreen = abs(windowRect.Left-mi.RcMonitor.Left) <= 4 &&
					abs(windowRect.Top-mi.RcMonitor.Top) <= 4 &&
					abs(windowRect.Right-mi.RcMonitor.Right) <= 4 &&
					abs(windowRect.Bottom-mi.RcMonitor.Bottom) <= 4
			}
		}
	}
	return info, nil
}

func windowTitle(handle windows.HWND) string {
	buffer := make([]uint16, 512)
	length, _, _ := getWindowTextW.Call(uintptr(handle), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return ""
	}
	return windows.UTF16ToString(buffer[:length])
}

var getWindowTextW = user32.NewProc("GetWindowTextW")

func abs(value int32) int32 {
	if value < 0 {
		return -value
	}
	return value
}
