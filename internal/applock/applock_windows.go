//go:build windows

// Package applock coordinates the foreground app and the background voice
// agent with a per-session named mutex.
package applock

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

const (
	mutexName           = `Local\JarvisAppRunning`
	agentMutexName      = `Local\JarvisVoiceAgent`
	supervisorMutexName = `Local\JarvisVoiceAgentSupervisor`
)

// Create takes the app-running mutex. The bool is true when another JARVIS
// instance already owns it; the returned release function still closes this
// handle.
func Create() (func(), bool, error) {
	return createNamed(mutexName)
}

// CreateAgent takes the single-instance mutex for the background voice agent.
// The bool is true when another agent already owns it; the returned release
// function still closes this handle.
func CreateAgent() (func(), bool, error) {
	return createNamed(agentMutexName)
}

// CreateAgentSupervisor takes the single-instance mutex for the supervisor
// that restarts the background agent after a crash.
func CreateAgentSupervisor() (func(), bool, error) {
	return createNamed(supervisorMutexName)
}

// createNamed is the shared implementation. Tests pass a unique name so they do
// not depend on whether a real JARVIS instance happens to be running.
func createNamed(name string) (func(), bool, error) {
	pointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, fmt.Errorf("encode mutex name: %w", err)
	}
	handle, createErr := windows.CreateMutex(nil, false, pointer)
	if createErr != nil && !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		return nil, false, fmt.Errorf("create mutex %q: %w", name, createErr)
	}
	release := func() {
		_ = windows.CloseHandle(handle)
	}
	return release, errors.Is(createErr, windows.ERROR_ALREADY_EXISTS), nil
}

// IsRunning reports whether the foreground app currently owns the mutex.
func IsRunning() bool {
	return mutexExists(mutexName)
}

// IsAgentRunning reports whether a background voice agent owns its mutex.
func IsAgentRunning() bool {
	return mutexExists(agentMutexName)
}

func mutexExists(name string) bool {
	pointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, pointer)
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}
