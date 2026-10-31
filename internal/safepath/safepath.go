// Package safepath resolves paths and checks them against an allowlist.
package safepath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Roots returns the directories the assistant may touch.
func Roots() []string {
	configDir, _ := os.UserConfigDir()
	home, _ := os.UserHomeDir()

	roots := make([]string, 0, 5)
	if configDir != "" {
		roots = append(roots, filepath.Join(configDir, "Jarvis"))
	}
	if tempDir := os.TempDir(); tempDir != "" {
		roots = append(roots,
			filepath.Join(tempDir, "jarvis_images"),
			filepath.Join(tempDir, "jarvis_screenshots"),
		)
	}
	if home != "" {
		roots = append(roots,
			filepath.Join(home, "Desktop"),
			filepath.Join(home, "Documents"),
		)
	}
	return roots
}

// Resolve checks path against the default allowlist.
func Resolve(path string) (string, bool) {
	return ResolveIn(path, Roots())
}

// ResolveIn returns the absolute path when it is inside one of roots.
// It rejects prefix tricks such as "Documents-old" and resolves symlinks and
// junction points where possible.
func ResolveIn(path string, roots []string) (string, bool) {
	path = strings.TrimSpace(strings.Trim(path, `"`))
	if path == "" {
		return "", false
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	resolved, ok := resolveExisting(abs)
	if !ok {
		return "", false
	}

	for _, root := range roots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rootResolved, ok := resolveExisting(rootAbs)
		if !ok {
			continue
		}

		rel, err := filepath.Rel(rootResolved, resolved)
		if err != nil || filepath.IsAbs(rel) {
			continue
		}
		if rel == "." {
			return resolved, true
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}
		return resolved, true
	}
	return "", false
}

func resolveExisting(path string) (string, bool) {
	missing := make([]string, 0, 4)
	current := path

	for {
		if info, err := os.Lstat(current); err == nil {
			// Junctions are reparse points that filepath.EvalSymlinks may
			// return unchanged. Reject them outright instead of appending a
			// missing descendant to a path that can leave the allowlist.
			if isReparsePoint(info) {
				return "", false
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", false
		}

		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved, true
		}

		// An existing component that EvalSymlinks cannot resolve is unsafe.
		// Refuse it instead of falling back to the unresolved path.
		if _, err := os.Lstat(current); err == nil {
			return "", false
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
