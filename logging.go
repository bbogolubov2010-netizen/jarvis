package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	appLogMaxBytes   = 2 << 20
	appLogBackups    = 3
	agentLogMaxBytes = 1 << 20
)

var logFileMu sync.Mutex

// appLog records startup and runtime problems in the per-user log directory.
// A packaged Wails application has no console, so stderr alone would silently
// discard every diagnostic. stderr is still written for `wails dev` and
// terminal runs.
func appLog(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, message)
	writeLogFile(filepath.Join(configDirPath(), "logs", "app.log"), message, appLogMaxBytes, appLogBackups)
}

// writeLogFile appends a timestamped line and rotates the file when it grows
// past maxBytes. Rotation keeps a bounded set of numbered backups instead of
// letting a long-running process fill the disk.
func writeLogFile(path, message string, maxBytes int64, backups int) {
	line := fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), message)

	logFileMu.Lock()
	defer logFileMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	rotateLogFile(path, maxBytes, backups)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(line)
}

func rotateLogFile(path string, maxBytes int64, backups int) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxBytes {
		return
	}
	if backups < 1 {
		_ = os.Remove(path)
		return
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, backups))
	for index := backups - 1; index >= 1; index-- {
		_ = os.Rename(
			fmt.Sprintf("%s.%d", path, index),
			fmt.Sprintf("%s.%d", path, index+1),
		)
	}
	_ = os.Rename(path, path+".1")
}
