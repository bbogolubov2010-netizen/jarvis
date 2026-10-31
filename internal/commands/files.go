package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"Jarvis/internal/safepath"
)

const (
	maxReadRunes   = 2000
	maxReadBytes   = maxReadRunes * 4
	maxListEntries = 500
)

var defaultListDir string

func init() {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		configDir = os.TempDir()
	}
	defaultListDir = filepath.Join(configDir, "Jarvis")
}

// RegisterFileCommands registers file management commands.
func RegisterFileCommands(m *Manager) {
	m.Register(Command{
		Name:        "mkdir",
		Aliases:     []string{"makedir", "createdir", "folder"},
		Description: "creates a directory inside Desktop, Documents, or JARVIS folders",
		Handler:     makeDir,
	})
	m.Register(Command{
		Name:        "touch",
		Aliases:     []string{"create", "newfile"},
		Description: "creates an empty file (never overwrites existing content)",
		Handler:     touchFile,
	})
	m.Register(Command{
		Name:        "write",
		Aliases:     []string{"save", "put"},
		Description: "writes text to a file: /write \"path\" content",
		Handler:     writeFile,
	})
	m.Register(Command{
		Name:        "ls",
		Aliases:     []string{"dir", "list", "files"},
		Description: "lists a directory inside the allowed folders",
		Handler:     listDir,
	})
	m.Register(Command{
		Name:        "cat",
		Aliases:     []string{"read", "view", "show"},
		Description: "prints a file from the allowed folders",
		Handler:     readFile,
	})
}

func makeDir(args string) string {
	path, ok := safepath.Resolve(args)
	if !ok {
		return "Access denied: path is empty or outside Desktop, Documents, and JARVIS folders."
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Sprintf("Error creating directory: %s", err)
	}
	return fmt.Sprintf("Directory created: %s", path)
}

func touchFile(args string) string {
	path, ok := safepath.Resolve(args)
	if !ok {
		return "Access denied: path is empty or outside Desktop, Documents, and JARVIS folders."
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Sprintf("Error creating parent directory: %s", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Sprintf("File already exists: %s", path)
		}
		return fmt.Sprintf("Error creating file: %s", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Sprintf("Error closing file: %s", err)
	}
	return fmt.Sprintf("File created: %s", path)
}

func writeFile(args string) string {
	rawPath, content, ok := splitPathAndContent(args)
	if !ok {
		return "Usage: /write <path> <content>\nUse quotes for paths with spaces: /write \"C:\\Users\\me\\Desktop\\note.txt\" text"
	}
	path, ok := safepath.Resolve(rawPath)
	if !ok {
		return "Access denied: path is outside Desktop, Documents, and JARVIS folders."
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Sprintf("Error creating parent directory: %s", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Sprintf("File already exists: %s", path)
		}
		return fmt.Sprintf("Error writing file: %s", err)
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Sprintf("Error writing file: %s", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Sprintf("Error writing file: %s", err)
	}
	return fmt.Sprintf("File written: %s (%d bytes)", path, len(content))
}

func splitPathAndContent(args string) (path, content string, ok bool) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", "", false
	}
	if strings.HasPrefix(args, `"`) {
		rest := args[1:]
		end := strings.Index(rest, `"`)
		if end < 0 {
			return "", "", false
		}
		path = strings.TrimSpace(rest[:end])
		content = strings.TrimSpace(rest[end+1:])
		return path, content, path != ""
	}
	parts := strings.SplitN(args, " ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), parts[0] != ""
}

func listDir(args string) string {
	path := defaultListDir
	if strings.TrimSpace(args) != "" {
		resolved, ok := safepath.Resolve(args)
		if !ok {
			return "Access denied: path is outside Desktop, Documents, and JARVIS folders."
		}
		path = resolved
	} else if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Sprintf("Error preparing directory: %s", err)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Sprintf("Error reading directory: %s", err)
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("Contents of %s:\n\n", path))
	if len(entries) == 0 {
		out.WriteString("  (empty)\n")
	}
	limit := len(entries)
	if limit > maxListEntries {
		limit = maxListEntries
	}
	for _, entry := range entries[:limit] {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		size := ""
		if info, infoErr := entry.Info(); infoErr == nil && !entry.IsDir() {
			size = fmt.Sprintf(" (%d bytes)", info.Size())
		}
		out.WriteString(fmt.Sprintf("  %s%s\n", name, size))
	}
	if len(entries) > maxListEntries {
		out.WriteString(fmt.Sprintf("\n  ... and %d more entries\n", len(entries)-maxListEntries))
	}
	return out.String()
}

func readFile(args string) string {
	path, ok := safepath.Resolve(args)
	if !ok {
		return "Access denied: path is empty or outside Desktop, Documents, and JARVIS folders."
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("Error reading file: %s", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxReadBytes+1))
	if err != nil {
		return fmt.Sprintf("Error reading file: %s", err)
	}
	truncated := len(data) > maxReadBytes
	if truncated {
		data = data[:maxReadBytes]
	}

	content := truncateRunes(strings.ToValidUTF8(string(data), ""), maxReadRunes)
	if truncated && !strings.HasSuffix(content, "... (truncated)") {
		content += "\n... (truncated)"
	}
	return fmt.Sprintf("File: %s\n\n%s", path, content)
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n... (truncated)"
}
