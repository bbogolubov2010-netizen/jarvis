// Package autostart manages the per-user Windows Run registry entry.
package autostart

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey             = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupApprovedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	appName            = "JARVIS"

	hiddenArgument          = "--hidden"
	agentArgument           = "--voice-agent"
	agentSupervisorArgument = "--voice-agent-supervisor"
)

// Mode describes how the Run entry starts JARVIS.
type Mode string

const (
	ModeNone            Mode = "none"
	ModeHidden          Mode = "hidden"
	ModeAgent           Mode = "agent"
	ModeAgentSupervisor Mode = "agent_supervisor"
	ModeOther           Mode = "other"
)

// Status is a diagnostic snapshot of the logon entry.
type Status struct {
	Enabled                 bool   `json:"enabled"`
	Mode                    Mode   `json:"mode"`
	RunValue                string `json:"run_value"`
	ExpectedValue           string `json:"expected_value"`
	Executable              string `json:"executable"`
	ExecutableExists        bool   `json:"executable_exists"`
	StartupApprovedDisabled bool   `json:"startup_approved_disabled"`
}

// Enable registers the executable to start hidden at logon. The full window is
// created but stays hidden in the tray, so voice, hotkey and the tray icon are
// available immediately.
func Enable() error {
	return Ensure(hiddenArgument)
}

// EnableAgent registers the supervised wake-word agent at logon. The full
// window is launched only after the wake word fires. It is kept for backwards
// compatibility; new installs use Enable.
func EnableAgent() error {
	return Ensure(agentSupervisorArgument)
}

// EnableAgentDirect registers the unsupported direct agent mode. It is kept
// for diagnostics and migration tests; normal installs use EnableAgent.
func EnableAgentDirect() error {
	return Ensure(agentArgument)
}

// Ensure writes the Run entry and clears a stale Windows "disabled" flag in
// StartupApproved so the entry actually runs at the next logon.
func Ensure(argument string) error {
	if err := enable(argument); err != nil {
		return err
	}
	if err := ClearStartupApproval(); err != nil {
		return fmt.Errorf("clear startup approval: %w", err)
	}
	return nil
}

func enable(argument string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Run registry key: %w", err)
	}
	defer key.Close()

	if err := key.SetStringValue(appName, fmt.Sprintf("\"%s\" %s", executable, argument)); err != nil {
		return fmt.Errorf("write Run registry value: %w", err)
	}
	return nil
}

// Disable removes the Run registry entry and its approval state.
func Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open Run registry key: %w", err)
	}
	defer key.Close()

	if err := key.DeleteValue(appName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("delete Run registry value: %w", err)
	}
	return ClearStartupApproval()
}

// ClearStartupApproval removes the Explorer "disabled" record for the entry.
// Windows keeps this value even after the Run value is rewritten, which is a
// common reason a valid Run entry never starts.
func ClearStartupApproval() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open StartupApproved key: %w", err)
	}
	defer key.Close()

	if err := key.DeleteValue(appName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("delete StartupApproved value: %w", err)
	}
	return nil
}

// IsEnabled reports whether the Run entry points at this executable in any
// supported mode.
func IsEnabled() bool {
	return IsHiddenEnabled() || IsAgentEnabled()
}

// IsHiddenEnabled reports whether the entry starts the hidden main window.
func IsHiddenEnabled() bool {
	return matches(hiddenArgument)
}

// IsAgentEnabled reports whether the entry uses the background agent,
// including the legacy unsupervised argument.
func IsAgentEnabled() bool {
	return matches(agentArgument) || matches(agentSupervisorArgument)
}

// IsAgentSupervisorEnabled reports whether the entry uses the supervised
// background agent.
func IsAgentSupervisorEnabled() bool {
	return matches(agentSupervisorArgument)
}

func matches(argument string) bool {
	value, err := runValue()
	if err != nil {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	expected := fmt.Sprintf("\"%s\" %s", executable, argument)
	return strings.EqualFold(strings.TrimSpace(value), expected)
}

func runValue() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()

	value, _, err := key.GetStringValue(appName)
	if err != nil {
		return "", err
	}
	return value, nil
}

// Inspect returns a diagnostic snapshot without changing anything.
func Inspect() Status {
	executable, _ := os.Executable()
	status := Status{
		Executable:    executable,
		Mode:          ModeNone,
		ExpectedValue: fmt.Sprintf("\"%s\" %s", executable, hiddenArgument),
	}
	if executable != "" {
		if info, err := os.Stat(executable); err == nil && !info.IsDir() {
			status.ExecutableExists = true
		}
	}

	if value, err := runValue(); err == nil {
		status.RunValue = value
		status.Mode = modeFor(value)
		status.Enabled = status.Mode != ModeNone && status.Mode != ModeOther
	}
	status.StartupApprovedDisabled = startupApprovedDisabled()
	return status
}

func modeFor(value string) Mode {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ModeNone
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasSuffix(lower, agentSupervisorArgument):
		return ModeAgentSupervisor
	case strings.HasSuffix(lower, agentArgument):
		return ModeAgent
	case strings.HasSuffix(lower, hiddenArgument):
		return ModeHidden
	default:
		return ModeOther
	}
}

func startupApprovedDisabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()

	value, _, err := key.GetBinaryValue(appName)
	if err != nil || len(value) == 0 {
		return false
	}
	// Explorer stores a 12-byte structure. The first byte is 0x02 or 0x06 for
	// enabled entries and 0x03 for entries disabled in Task Manager.
	return value[0]&0x01 == 0x01
}
