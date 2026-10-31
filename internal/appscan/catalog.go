// Package appscan builds a catalog of applications installed on the machine.
//
// The assistant used to rely on a short hardcoded list, which meant most
// installed programs could not be opened by name. The catalog combines Start
// Menu shortcuts, registered App Paths, Start-menu app IDs (including UWP), and
// a bounded Program Files scan. It is cached in %AppData%\Jarvis\apps.json so
// startup stays fast; a refresh runs in the background.
package appscan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

var errUnsupported = errors.New("application launching is only supported on Windows")

// catalogVersion is bumped when the on-disk format changes.
const catalogVersion = 2

// maxApps caps the catalog so a machine with thousands of executables cannot
// make the JSON file or the settings list unmanageable.
const maxApps = 600

// App is one launchable entry.
type App struct {
	Name    string   `json:"name"`
	Path    string   `json:"path,omitempty"`
	Args    []string `json:"args,omitempty"`
	AUMID   string   `json:"aumid,omitempty"`
	Source  string   `json:"source"`
	Aliases []string `json:"aliases,omitempty"`
}

// Catalog is the persisted application index.
type Catalog struct {
	Version   int       `json:"version"`
	ScannedAt time.Time `json:"scanned_at"`
	Apps      []App     `json:"apps"`
}

// DefaultPath returns %AppData%\Jarvis\apps.json.
func DefaultPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "Jarvis", "apps.json")
}

// Load reads a catalog. A missing file is not an error.
func Load(path string) (Catalog, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Catalog{Version: catalogVersion}, nil
	}
	if err != nil {
		return Catalog{}, fmt.Errorf("read app catalog: %w", err)
	}
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("parse app catalog: %w", err)
	}
	if catalog.Version != catalogVersion {
		return Catalog{Version: catalogVersion}, nil
	}
	return catalog, nil
}

// Save writes the catalog atomically.
func Save(path string, catalog Catalog) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create app catalog directory: %w", err)
	}
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".apps-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

var (
	cacheMu  sync.RWMutex
	cache    Catalog
	cacheOK  bool
	scanMu   sync.Mutex
	scanLast time.Time
)

// Cached returns the last catalog loaded from disk. It never blocks on a scan.
func Cached() Catalog {
	cacheMu.RLock()
	if cacheOK {
		catalog := cache
		cacheMu.RUnlock()
		return catalog
	}
	cacheMu.RUnlock()

	catalog, err := Load(DefaultPath())
	if err != nil {
		catalog = Catalog{Version: catalogVersion}
	}
	SetCatalog(catalog)
	return catalog
}

// SetCatalog replaces the in-memory catalog.
func SetCatalog(catalog Catalog) {
	cacheMu.Lock()
	cache = catalog
	cacheOK = true
	cacheMu.Unlock()
}

// Refresh scans the machine and updates the cache. Concurrent calls share one
// scan instead of starting several.
func Refresh(ctx context.Context) (Catalog, error) {
	scanMu.Lock()
	defer scanMu.Unlock()

	catalog, err := Scan(ctx)
	if err != nil {
		return Catalog{}, err
	}
	if err := Save(DefaultPath(), catalog); err != nil {
		return catalog, err
	}
	SetCatalog(catalog)
	scanLast = time.Now()
	return catalog, nil
}

// RefreshIfStale refreshes the catalog when it is missing or older than maxAge.
// It is safe to call from a background goroutine at startup.
func RefreshIfStale(ctx context.Context, maxAge time.Duration) error {
	catalog := Cached()
	if catalog.ScannedAt.IsZero() || time.Since(catalog.ScannedAt) > maxAge {
		_, err := Refresh(ctx)
		return err
	}
	return nil
}

// Scan builds a fresh catalog from every available source.
func Scan(ctx context.Context) (Catalog, error) {
	high := make([]App, 0, 128)
	high = append(high, scanStartMenu(ctx)...)
	high = append(high, scanAppPaths()...)
	low := scanProgramFiles(ctx)

	apps := dedupe(high, low)
	if len(apps) > maxApps {
		// dedupe keeps high-quality sources first, so trimming drops
		// filesystem-only entries rather than Start Menu ones.
		apps = apps[:maxApps]
	}
	sort.Slice(apps, func(i, j int) bool {
		return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
	})
	return Catalog{Version: catalogVersion, ScannedAt: time.Now(), Apps: apps}, nil
}

// dedupe merges the high-quality sources (Start Menu, App Paths) with the
// filesystem scan. Duplicate names keep the entry from the better source, and
// filesystem-only entries fill the remaining slots.
func dedupe(high, low []App) []App {
	result := make([]App, 0, len(high)+len(low))
	byPath := make(map[string]int, len(high)+len(low))
	byName := make(map[string]int, len(high)+len(low))

	add := func(app App) {
		app.Name = strings.TrimSpace(app.Name)
		if app.Name == "" || (app.Path == "" && app.AUMID == "") {
			return
		}
		if isVersionDir(app.Name) || isJunkName(app.Name) {
			return
		}
		pathKey := strings.ToLower(app.Path)
		if pathKey == "" {
			pathKey = "aumid:" + strings.ToLower(app.AUMID)
		}
		nameKey := normalize(app.Name)

		if index, ok := byPath[pathKey]; ok {
			if sourceRank(app.Source) > sourceRank(result[index].Source) {
				result[index] = app
			}
			return
		}
		if index, ok := byName[nameKey]; ok {
			if sourceRank(app.Source) > sourceRank(result[index].Source) {
				result[index] = app
				byPath[pathKey] = index
			}
			return
		}
		byPath[pathKey] = len(result)
		byName[nameKey] = len(result)
		result = append(result, app)
	}

	for _, app := range high {
		add(app)
	}
	for _, app := range low {
		add(app)
	}
	return result
}

func sourceRank(source string) int {
	switch source {
	case "start-menu", "start-apps":
		return 3
	case "app-paths":
		return 2
	default:
		return 1
	}
}

// isJunkName filters entries that are clearly not user-facing applications.
func isJunkName(name string) bool {
	lower := strings.ToLower(name)
	for _, token := range []string{
		"update-", "readme", "license", "eula", "changelog", "unins",
		"installer", "setup", "acrobatinfo", "manual", "recovery",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	switch lower {
	case "about", "about java", "accessories", "ads", "documentation", "help", "applicationontology":
		return true
	}
	return false
}

// Lookup finds the best application for a spoken or typed name.
func Lookup(query string) (App, bool) {
	return Match(Cached().Apps, query)
}

// Match returns the best catalog entry for query and whether the match is
// confident enough to launch.
func Match(apps []App, query string) (App, bool) {
	normalized := normalize(query)
	if normalized == "" {
		return App{}, false
	}
	best := -1
	var bestApp App
	for _, app := range apps {
		score := scoreApp(app, normalized)
		if score > best {
			best = score
			bestApp = app
		}
	}
	if best < 40 {
		return App{}, false
	}
	return bestApp, true
}

// Suggestions returns up to limit close matches for an unknown name.
func Suggestions(apps []App, query string, limit int) []string {
	normalized := normalize(query)
	type scored struct {
		name  string
		score int
	}
	var matches []scored
	for _, app := range apps {
		score := scoreApp(app, normalized)
		if score > 0 {
			matches = append(matches, scored{name: app.Name, score: score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].name < matches[j].name
		}
		return matches[i].score > matches[j].score
	})
	names := make([]string, 0, limit)
	seen := make(map[string]bool, limit)
	for _, match := range matches {
		if seen[strings.ToLower(match.name)] {
			continue
		}
		seen[strings.ToLower(match.name)] = true
		names = append(names, match.name)
		if len(names) >= limit {
			break
		}
	}
	return names
}

func scoreApp(app App, query string) int {
	names := append([]string{app.Name}, app.Aliases...)
	best := 0
	for _, name := range names {
		if score := scoreName(normalize(name), query); score > best {
			best = score
		}
	}
	return best
}

func scoreName(name, query string) int {
	if name == "" || query == "" {
		return 0
	}
	if name == query {
		return 100
	}
	if compact(name) == compact(query) {
		return 95
	}
	nameTokens := strings.Fields(name)
	queryTokens := strings.Fields(query)
	if containsTokens(nameTokens, queryTokens) {
		if len(queryTokens) == len(nameTokens) {
			return 85
		}
		return 70
	}
	if strings.HasPrefix(name, query) {
		return 60
	}
	if strings.Contains(name, query) {
		return 50
	}
	// Tolerate a small typo or a missing word, but never for very short names.
	if len(query) >= 4 && levenshtein(name, query) <= 2 {
		return 45
	}
	return 0
}

func containsTokens(haystack, needles []string) bool {
	for _, needle := range needles {
		found := false
		for _, hay := range haystack {
			if hay == needle {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return len(needles) > 0
}

func compact(s string) string {
	return strings.ReplaceAll(s, " ", "")
}

// isVersionDir reports whether a name is only a version number, such as "1.0",
// "156.0.4314.50", or "2024". Executables in those folders are almost always
// helpers or update payloads.
func isVersionDir(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	digits := 0
	for _, r := range name {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.' || r == '_' || r == '-' || r == ' ':
		default:
			return false
		}
	}
	return digits > 0
}

// normalize lowercases text and strips punctuation, keeping letters and digits.
func normalize(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "ё", "е")
	var b strings.Builder
	b.Grow(len(text))
	lastSpace := false
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		case unicode.IsSpace(r) || unicode.IsPunct(r):
			if !lastSpace && b.Len() > 0 {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := 0; j <= len(br); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

// Launch starts the application.
func Launch(app App) error {
	if app.AUMID != "" {
		return launchAUMID(app.AUMID)
	}
	if app.Path == "" {
		return errors.New("application has no launch target")
	}
	if !filepath.IsAbs(app.Path) {
		return fmt.Errorf("application path is not absolute: %s", app.Path)
	}
	info, err := os.Stat(app.Path)
	if err != nil {
		return fmt.Errorf("application is missing: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("application path is a directory: %s", app.Path)
	}
	command := execCommand(app.Path, app.Args...)
	if err := command.Start(); err != nil {
		return err
	}
	if command.Process != nil {
		_ = command.Process.Release()
	}
	return nil
}
