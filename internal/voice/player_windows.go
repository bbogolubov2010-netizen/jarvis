//go:build windows

package voice

import (
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndFilename  = 0x00020000
)

var (
	winmm          = windows.NewLazySystemDLL("winmm.dll")
	procPlaySoundW = winmm.NewProc("PlaySoundW")

	winmmOnce sync.Once
	winmmErr  error
)

// Player plays short WAV files asynchronously through the default Windows
// playback device. It is safe for concurrent use.
type Player struct {
	dirs       []string
	enabled    func() bool
	mu         sync.Mutex
	startMu    sync.Mutex
	generation uint64
}

// NewPlayer creates a player that searches dirs in order. enabled is checked
// before every sound; a nil callback means the player is enabled.
func NewPlayer(dirs []string, enabled func() bool) *Player {
	return &Player{dirs: uniquePaths(dirs), enabled: enabled}
}

// Play starts the phrase for key. A new phrase interrupts the previous one.
// It reports whether a playable file was found and playback was requested.
func (p *Player) Play(key Key) bool {
	if p == nil || !p.isEnabled() {
		return false
	}
	path := p.resolve(key)
	if path == "" {
		return false
	}

	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}

	// Hold startMu and mu across the short asynchronous call so Stop either
	// runs before this playback (and wins) or after it (and interrupts it).
	p.startMu.Lock()
	defer p.startMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	return playSound(pointer, sndAsync|sndNoDefault|sndFilename)
}

// PlaySync plays a phrase and waits for it to finish. It is intended for
// shutdown paths where the process is about to exit.
func (p *Player) PlaySync(key Key) bool {
	if p == nil || !p.isEnabled() {
		return false
	}
	path := p.resolve(key)
	if path == "" {
		return false
	}
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}

	// Do not hold mu while blocking: Stop must be able to interrupt this
	// playback from another goroutine. The generation check closes the small
	// window between a Stop and the synchronous PlaySound call.
	p.startMu.Lock()
	p.mu.Lock()
	generation := p.generation
	p.mu.Unlock()
	p.startMu.Unlock()
	if !p.isCurrent(generation) {
		return false
	}
	return playSound(pointer, sndNoDefault|sndFilename)
}

// Stop interrupts the current phrase.
func (p *Player) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.generation++
	p.mu.Unlock()
	// Do not take startMu: PlaySync can be blocked in the Windows call and
	// must still be interruptible.
	playSound(nil, 0)
}

func (p *Player) isCurrent(generation uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.generation == generation
}

// PlayFileAsync plays an arbitrary WAV file asynchronously. It is used by the
// text-to-speech layer for generated Piper output.
func PlayFileAsync(path string) bool {
	return playFile(path, sndAsync|sndNoDefault|sndFilename)
}

// PlayFileSync plays an arbitrary WAV file and waits for it to finish.
func PlayFileSync(path string) bool {
	return playFile(path, sndNoDefault|sndFilename)
}

// StopFile interrupts any playback started through the winmm PlaySound API.
func StopFile() {
	playSound(nil, 0)
}

func playFile(path string, flags uint32) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	return playSound(pointer, flags)
}

// Available reports whether a file for key exists in one of the search dirs.
func (p *Player) Available(key Key) bool {
	return p != nil && p.resolve(key) != ""
}

func (p *Player) isEnabled() bool {
	return p.enabled == nil || p.enabled()
}

func (p *Player) resolve(key Key) string {
	names := FilesFor(key)
	if len(names) == 0 {
		return ""
	}

	// Start at a random variant, then fall back through the rest in case a
	// particular file is missing.
	start := 0
	if len(names) > 1 {
		start = rand.Intn(len(names))
	}
	for offset := 0; offset < len(names); offset++ {
		name := names[(start+offset)%len(names)]
		for _, dir := range p.dirs {
			path := filepath.Join(dir, name)
			info, err := os.Stat(path)
			if err == nil && !info.IsDir() {
				return path
			}
		}
	}
	return ""
}

func playSound(pointer *uint16, flags uint32) bool {
	winmmOnce.Do(func() {
		winmmErr = procPlaySoundW.Find()
	})
	if winmmErr != nil {
		return false
	}
	result, _, _ := procPlaySoundW.Call(uintptr(unsafe.Pointer(pointer)), 0, uintptr(flags))
	return result != 0
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		cleaned := filepath.Clean(path)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}
