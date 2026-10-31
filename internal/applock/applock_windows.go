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
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, false, fmt.Errorf("encode mutex name: %w", err)
	}
	handle, createErr := windows.CreateMutex(nil, false, name)
	if createErr != nil && !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		return nil, false, fmt.Errorf("create app mutex: %w", createErr)
	}
	release := func() {
		_ = windows.CloseHandle(handle)
	}
	return release, errors.Is(createErr, windows.ERROR_ALREADY_EXISTS), nil
}

// CreateAgent takes the single-instance mutex for the background voice agent.
// The bool is true when another agent already owns it; the returned release
// function still closes this handle.
func CreateAgent() (func(), bool, error) {
	name, err := windows.UTF16PtrFromString(agentMutexName)
	if err != nil {
		return nil, false, fmt.Errorf("encode agent mutex name: %w", err)
	}
	handle, createErr := windows.CreateMutex(nil, false, name)
	if createErr != nil && !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		return nil, false, fmt.Errorf("create agent mutex: %w", createErr)
	}
	release := func() {
		_ = windows.CloseHandle(handle)
	}
	return release, errors.Is(createErr, windows.ERROR_ALREADY_EXISTS), nil
}

// CreateAgentSupervisor takes the single-instance mutex for the supervisor
// that restarts the background agent after a crash.
func CreateAgentSupervisor() (func(), bool, error) {
	name, err := windows.UTF16PtrFromString(supervisorMutexName)
	if err != nil {
		return nil, false, fmt.Errorf("encode agent supervisor mutex name: %w", err)
	}
	handle, createErr := windows.CreateMutex(nil, false, name)
	if createErr != nil && !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		return nil, false, fmt.Errorf("create agent supervisor mutex: %w", createErr)
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
