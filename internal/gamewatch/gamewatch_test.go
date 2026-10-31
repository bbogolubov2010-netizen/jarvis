package gamewatch

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAddListForget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games.json")
	watcher, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	game := watcher.Add("Test Game", `C:\Games\TestGame.exe`)
	if game.Exe != "testgame.exe" {
		t.Fatalf("Exe = %q", game.Exe)
	}
	if !hasGame(watcher.List(), "testgame.exe") {
		t.Fatalf("added game missing from list: %#v", watcher.List())
	}

	reloaded, err := New(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !hasGame(reloaded.List(), "testgame.exe") {
		t.Fatal("game was not persisted")
	}

	if !reloaded.Forget("testgame.exe") {
		t.Fatal("Forget returned false")
	}
	if hasGame(reloaded.List(), "testgame.exe") {
		t.Fatal("game still present after Forget")
	}
}

func TestDisplayName(t *testing.T) {
	cases := map[string]string{
		`C:\Games\cyberpunk_2077.exe`: "Cyberpunk 2077",
		`C:\Games\half-life2.exe`:     "Half life2",
		"":                            "Unknown game",
	}
	for input, want := range cases {
		if got := displayName(input); got != want {
			t.Errorf("displayName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLooksLikeGame(t *testing.T) {
	if looksLikeGame(windowInfo{Exe: "chrome.exe", Path: `C:\Program Files\Chrome\chrome.exe`}) {
		t.Fatal("browser must not be treated as a game")
	}
	if looksLikeGame(windowInfo{Exe: "game.exe", Path: `C:\Windows\System32\game.exe`, Fullscreen: true}) {
		t.Fatal("system binaries must not be treated as games")
	}
	if !looksLikeGame(windowInfo{Exe: "game.exe", Path: `D:\Games\game.exe`, Fullscreen: true}) {
		t.Fatal("fullscreen game was not detected")
	}
	if !looksLikeGame(windowInfo{Exe: "game.exe", Path: `D:\Games\game.exe`, HasCaption: false}) {
		t.Fatal("caption-less game window was not detected")
	}
}

func hasGame(games []Game, exe string) bool {
	for _, game := range games {
		if strings.EqualFold(game.Exe, exe) {
			return true
		}
	}
	return false
}
