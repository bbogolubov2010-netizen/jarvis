package appscan

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMatchExactAndAlias(t *testing.T) {
	apps := []App{
		{Name: "Google Chrome"},
		{Name: "Notepad"},
		{Name: "Visual Studio Code", Aliases: []string{"vscode", "code"}},
	}
	cases := []struct {
		query string
		want  string
	}{
		{"Google Chrome", "Google Chrome"},
		{"google chrome", "Google Chrome"},
		{"chrome", "Google Chrome"},
		{"блокнот", ""},
		{"notepad", "Notepad"},
		{"code", "Visual Studio Code"},
		{"visual studio code", "Visual Studio Code"},
	}
	for _, tc := range cases {
		app, ok := Match(apps, tc.query)
		if tc.want == "" {
			if ok {
				t.Errorf("Match(%q) unexpectedly returned %q", tc.query, app.Name)
			}
			continue
		}
		if !ok || app.Name != tc.want {
			t.Errorf("Match(%q) = %q, %v; want %q", tc.query, app.Name, ok, tc.want)
		}
	}
}

func TestMatchToleratesTypo(t *testing.T) {
	apps := []App{{Name: "Telegram"}}
	if app, ok := Match(apps, "telegrm"); !ok || app.Name != "Telegram" {
		t.Fatalf("typo match failed: %q %v", app.Name, ok)
	}
}

func TestMatchRejectsUnrelated(t *testing.T) {
	apps := []App{{Name: "Google Chrome"}, {Name: "Steam"}}
	if app, ok := Match(apps, "photoshop"); ok {
		t.Fatalf("unrelated query matched %q", app.Name)
	}
}

func TestSuggestions(t *testing.T) {
	apps := []App{{Name: "Google Chrome"}, {Name: "Chromium"}, {Name: "Steam"}}
	got := Suggestions(apps, "chrom", 2)
	if len(got) != 2 {
		t.Fatalf("Suggestions returned %v", got)
	}
}

func TestLaunchRejectsRelativePath(t *testing.T) {
	if err := Launch(App{Name: "bad", Path: "notepad.exe"}); err == nil {
		t.Fatal("relative path was accepted")
	}
}

func TestScanSmoke(t *testing.T) {
	if os.Getenv("JARVIS_APPTEST") != "1" {
		t.Skip("set JARVIS_APPTEST=1 to run a live application scan")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	catalog, err := Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("scanned %d applications", len(catalog.Apps))
	if len(catalog.Apps) < 5 {
		t.Fatalf("expected at least five applications, got %d", len(catalog.Apps))
	}
	names := make([]string, 0, 20)
	for i, app := range catalog.Apps {
		if i < 20 {
			names = append(names, app.Name)
		}
	}
	t.Logf("sample: %v", names)
}
