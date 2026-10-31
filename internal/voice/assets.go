package voice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureAssets copies every phrase WAV that is missing from dest using the
// first source directory that contains it. Existing files are never
// overwritten, which lets a user replace a phrase without it being reset on
// the next launch.
func EnsureAssets(dest string, sources []string) error {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return errors.New("voice directory is empty")
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return fmt.Errorf("create voice directory: %w", err)
	}

	missing := missingIn(dest)
	if len(missing) == 0 {
		return nil
	}

	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" || samePath(source, dest) {
			continue
		}
		for _, name := range missing {
			sourcePath := filepath.Join(source, name)
			info, err := os.Stat(sourcePath)
			if err != nil || info.IsDir() {
				continue
			}
			if err := copyFile(sourcePath, filepath.Join(dest, name)); err != nil {
				return fmt.Errorf("copy voice file %s: %w", name, err)
			}
		}
		missing = missingIn(dest)
		if len(missing) == 0 {
			return nil
		}
	}

	return fmt.Errorf("voice pack incomplete: missing %d file(s)", len(missing))
}

// Missing returns the phrase files that are not present in any of dirs.
func Missing(dirs []string) []string {
	missing := make([]string, 0)
	for _, name := range AllFiles() {
		found := false
		for _, dir := range dirs {
			if dir == "" {
				continue
			}
			info, err := os.Stat(filepath.Join(dir, name))
			if err == nil && !info.IsDir() {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, name)
		}
	}
	return missing
}

func missingIn(dir string) []string {
	missing := make([]string, 0)
	for _, name := range AllFiles() {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			missing = append(missing, name)
		}
	}
	return missing
}

func copyFile(source, dest string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o600)
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return strings.EqualFold(filepath.Clean(absA), filepath.Clean(absB))
}
