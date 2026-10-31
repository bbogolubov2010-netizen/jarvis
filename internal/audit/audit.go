// Package audit writes an append-only local log of actions and reads it back.
package audit

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxLogValueLength = 2000
	maxTailBytes      = 2 << 20
	tailLines         = 30
	maxLogFiles       = 3
	retentionDays     = 30
)

// Logger serializes writes to a single daily log file.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	dir  string
	date string
}

var (
	mu     sync.RWMutex
	global *Logger
	logDir string
)

// Init opens (or creates) today's log file under configDir/logs.
func Init(configDir string) error {
	dir := filepath.Join(configDir, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create audit directory: %w", err)
	}
	pruneOldLogs(dir, time.Now().AddDate(0, 0, -retentionDays))
	path := filepath.Join(dir, time.Now().Format("2006-01-02")+".log")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}

	logger := &Logger{
		file: file,
		dir:  dir,
		date: time.Now().Format("2006-01-02"),
	}
	mu.Lock()
	previous := global
	global = logger
	logDir = dir
	mu.Unlock()

	if previous != nil {
		previous.close()
	}
	return nil
}

func pruneOldLogs(dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// Close flushes and releases the current log file.
func Close() {
	mu.Lock()
	logger := global
	global = nil
	mu.Unlock()
	if logger != nil {
		logger.close()
	}
}

func (l *Logger) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}

// Log appends a single line. It never panics when logging is unavailable.
func Log(action, details string) {
	mu.RLock()
	logger := global
	mu.RUnlock()
	if logger == nil {
		return
	}

	now := time.Now()
	date := now.Format("2006-01-02")
	line := fmt.Sprintf("[%s] %s: %s\n",
		now.Format("2006-01-02 15:04:05"),
		action,
		sanitizeValue(details),
	)

	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.file == nil {
		return
	}

	// A long-running process must not keep appending to yesterday's file.
	if date != logger.date {
		path := filepath.Join(logger.dir, date+".log")
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "audit: rotate failed: %v\n", err)
		} else {
			_ = logger.file.Close()
			logger.file = file
			logger.date = date
		}
	}

	if _, err := io.WriteString(logger.file, line); err != nil {
		fmt.Fprintf(os.Stderr, "audit: write failed: %v\n", err)
	}
}

// GetLogs returns the tail of the most recent log files. It never closes the
// active writer, so logging keeps working after the call.
func GetLogs() string {
	mu.RLock()
	dir := logDir
	mu.RUnlock()
	if dir == "" {
		return "Audit log is not initialized."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "Error reading audit logs."
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) > maxLogFiles {
		names = names[len(names)-maxLogFiles:]
	}
	if len(names) == 0 {
		return "No audit logs found."
	}

	var out strings.Builder
	out.WriteString("Audit Log:\n\n")
	for _, name := range names {
		lines, err := tail(filepath.Join(dir, name), tailLines)
		if err != nil {
			continue
		}
		out.WriteString(fmt.Sprintf("--- %s ---\n", name))
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				out.WriteString(line + "\n")
			}
		}
		out.WriteString("\n")
	}
	return out.String()
}

func tail(path string, lines int) ([]string, error) {
	if lines <= 0 {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := int64(0)
	if info.Size() > maxTailBytes {
		start = info.Size() - maxTailBytes
	}
	if start > 0 {
		if _, err := file.Seek(start, io.SeekStart); err != nil {
			return nil, err
		}
	}

	ring := make([]string, 0, lines)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	skipPartial := start > 0
	for scanner.Scan() {
		if skipPartial {
			// The seek may land in the middle of a line.
			skipPartial = false
			continue
		}
		if len(ring) == lines {
			ring = append(ring[1:], scanner.Text())
			continue
		}
		ring = append(ring, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ring, nil
}

func sanitizeValue(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	if len(value) > maxLogValueLength {
		value = validPrefix(value, maxLogValueLength) + "..."
	}
	return value
}

// validPrefix cuts text to at most max bytes without splitting a UTF-8 rune.
func validPrefix(text string, max int) string {
	if len(text) <= max {
		return text
	}
	text = text[:max]
	for len(text) > 0 && !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}

// LogOpen records an application launch.
func LogOpen(app string) { Log("OPEN", app) }

// LogSearch records a web search.
func LogSearch(query string) { Log("SEARCH", "Query: "+query) }

// LogScreenshot records a screenshot action.
func LogScreenshot() { Log("SCREENSHOT", "Screenshot taken") }

// LogSmartSearch records a browser-based search.
func LogSmartSearch(query string) { Log("SMART_SEARCH", "Query: "+query) }

// LogCommand records a command execution.
func LogCommand(cmd, args string) { Log("COMMAND", cmd+" "+args) }

// LogDenied records a blocked action.
func LogDenied(action, reason string) { Log("DENIED", action+" - "+reason) }

// LogInjectionBlocked records a prompt-injection attempt.
func LogInjectionBlocked(source string) {
	Log("INJECTION_BLOCKED", "Neutralized input from: "+source)
}
