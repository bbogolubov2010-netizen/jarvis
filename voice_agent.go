package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"Jarvis/internal/applock"
	"Jarvis/internal/config"
	"Jarvis/internal/ipc"
	"Jarvis/internal/voice"
	"Jarvis/internal/voiceengine"
	"Jarvis/internal/voiceinput"
)

// runVoiceAgent is the small always-on listener used when JARVIS is closed. It
// owns the microphone only while the main window is not running, and launches
// the full app with a Wails single-instance message when the wake word fires.
func runVoiceAgent() {
	release, alreadyRunning, err := applock.CreateAgent()
	if err != nil {
		agentLog("agent lock: %v", err)
		return
	}
	defer release()
	if alreadyRunning {
		agentLog("another voice agent is already running; exiting")
		return
	}

	verifiedRoot := ""
	var (
		verifyErr  error
		nextVerify time.Time
	)
	for {
		settings, _, err := config.Load(configPath())
		if err != nil {
			agentLog("settings: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}
		if settings.VoiceInputDisabled || settings.VoiceAgentDisabled || settings.WakeWordDisabled {
			agentLog("voice agent disabled; exiting")
			return
		}

		paths := voiceengine.Resolve()
		if !paths.VoskReady() || !paths.WhisperReady() {
			agentLog("voice engine is incomplete at %s; retrying", paths.Root)
			time.Sleep(15 * time.Second)
			continue
		}
		// Re-check a failing engine once a minute so repairing it in place does
		// not require a restart. A healthy engine is hashed only once per root.
		if verifiedRoot != paths.Root || (verifyErr != nil && time.Now().After(nextVerify)) {
			verifiedRoot = paths.Root
			verifyErr = paths.Verify()
			nextVerify = time.Now().Add(60 * time.Second)
			if verifyErr != nil {
				agentLog("voice engine integrity: %v", verifyErr)
			}
		}
		if verifyErr != nil {
			time.Sleep(30 * time.Second)
			continue
		}

		if applock.IsRunning() {
			time.Sleep(2 * time.Second)
			continue
		}

		player := voice.NewPlayer(agentVoiceDirs(), func() bool { return !settings.VoiceDisabled })
		if err := voice.EnsureAssets(filepath.Join(configDirPath(), "voice"), agentVoiceSourceDirs()); err != nil {
			agentLog("voice assets: %v", err)
		}

		launched := make(chan struct{})
		var (
			launchMu     sync.Mutex
			launchClosed bool
		)
		launch := func(args ...string) {
			launchMu.Lock()
			defer launchMu.Unlock()
			if launchClosed {
				return
			}
			if err := startJarvis(args...); err != nil {
				// Leave the channel open so a later wake or command can retry.
				// A transient failure must not disable the agent for the rest
				// of this listening session.
				agentLog("launch app: %v", err)
				return
			}
			launchClosed = true
			close(launched)
		}
		sessionDone := make(chan struct{})

		callbacks := voiceinput.Callbacks{
			OnState: func(state voiceinput.State) {
				agentLog("state: %s", state)
			},
			OnWake: func(rest string) {
				if player != nil {
					_ = player.PlaySync(voice.KeyAck)
				}
				// If the user only said the wake word, open the app and let it
				// listen. If a command was heard in the same utterance, give
				// Whisper time to transcribe it; OnCommand launches the app with
				// the command and closes launched. The longer fallback covers a
				// failed transcription without losing the wake gesture.
				delay := 4 * time.Second
				if rest != "" {
					delay = 12 * time.Second
				}
				go func() {
					timer := time.NewTimer(delay)
					defer timer.Stop()
					select {
					case <-launched:
					case <-sessionDone:
					case <-timer.C:
						launch("--voice-wake")
					}
				}()
			},
			OnCommand: func(text string) bool {
				// Launch the app without putting the command on its command
				// line, then deliver it over the authenticated loopback IPC
				// channel. SendCommand waits for the app to publish its
				// endpoint, so this also works on a cold start.
				launch()
				command := text
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := ipc.SendCommand(ctx, configDirPath(), command); err != nil {
						agentLog("send command: %v", err)
					}
				}()
				return false
			},
			OnTranscript: func(text string) {
				agentLogVoice("transcript", text)
			},
			OnVosk: func(text string) {
				agentLogVoice("vosk", text)
			},
			OnError: func(message string) {
				agentLog("error: %s", message)
			},
		}

		manager := voiceinput.NewManager(paths, settings.VoiceDeviceID, callbacks)
		manager.SetWakeWord(settings.VoiceWakeWord)
		if err := manager.Start(); err != nil {
			agentLog("start listener: %v", err)
			time.Sleep(15 * time.Second)
			continue
		}
		agentLog("listening for wake word")

		// The user may launch JARVIS manually while the agent is listening. The
		// app owns the microphone in that case, so release it promptly.
		appStarted := make(chan struct{})
		stopWatch := make(chan struct{})
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopWatch:
					return
				case <-ticker.C:
					if applock.IsRunning() {
						close(appStarted)
						return
					}
				}
			}
		}()

		launchedApp := false
		select {
		case <-launched:
			launchedApp = true
		case <-appStarted:
			agentLog("foreground app started; releasing microphone")
		}
		close(sessionDone)
		close(stopWatch)
		manager.Stop()

		// The app needs a moment to create its mutex and open the microphone.
		// Wait for that handoff so both processes do not race for the device.
		if launchedApp {
			deadline := time.Now().Add(20 * time.Second)
			for time.Now().Before(deadline) {
				if applock.IsRunning() {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
		}
		time.Sleep(2 * time.Second)
	}
}

// runVoiceAgentSupervisor keeps the background agent alive after a crash. It
// is the mode registered in the Run key so a transient model or device failure
// does not permanently disable the wake word.
func runVoiceAgentSupervisor() {
	release, alreadyRunning, err := applock.CreateAgentSupervisor()
	if err != nil {
		agentLog("agent supervisor lock: %v", err)
		return
	}
	defer release()
	if alreadyRunning {
		agentLog("another voice agent supervisor is already running; exiting")
		return
	}

	executable, err := os.Executable()
	if err != nil {
		agentLog("locate executable: %v", err)
		return
	}

	backoff := 2 * time.Second
	for {
		settings, _, loadErr := config.Load(configPath())
		if loadErr == nil && (settings.VoiceInputDisabled || settings.VoiceAgentDisabled || settings.WakeWordDisabled) {
			agentLog("voice agent disabled; supervisor exiting")
			return
		}
		if loadErr != nil {
			agentLog("settings: %v; supervisor will retry", loadErr)
		}

		if applock.IsAgentRunning() {
			time.Sleep(5 * time.Second)
			continue
		}

		command := exec.Command(executable, "--voice-agent")
		if err := command.Start(); err != nil {
			agentLog("start agent: %v", err)
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		started := time.Now()
		waitErr := command.Wait()
		if waitErr != nil {
			agentLog("agent exited: %v", waitErr)
		} else {
			agentLog("agent exited")
		}
		if time.Since(started) > 2*time.Minute {
			backoff = 2 * time.Second
		}
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func startJarvis(args ...string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	command := exec.Command(executable, args...)
	if err := command.Start(); err != nil {
		return err
	}
	if command.Process != nil {
		_ = command.Process.Release()
	}
	return nil
}

func agentVoiceDirs() []string {
	dirs := []string{filepath.Join(configDirPath(), "voice")}
	if executable, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(executable)
		dirs = append(dirs,
			filepath.Join(exeDir, "voice"),
			filepath.Join(exeDir, "..", "..", "voice"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(cwd, "voice"))
	}
	return dirs
}

func agentVoiceSourceDirs() []string {
	dirs := agentVoiceDirs()
	if len(dirs) <= 1 {
		return nil
	}
	return dirs[1:]
}

// agentLogVoice records only metadata by default. Raw speech is sensitive and
// is not written to the plaintext agent log unless the user explicitly opts
// in for debugging with JARVIS_VOICE_DEBUG=1.
func agentLogVoice(kind, text string) {
	if os.Getenv("JARVIS_VOICE_DEBUG") == "1" {
		agentLog("%s: %q", kind, text)
		return
	}
	agentLog("%s: %d chars", kind, utf8.RuneCountInString(text))
}

// agentLog keeps the background listener's diagnostics in a rotating file.
// The agent may run without any visible window, so stderr is not enough.
func agentLog(format string, args ...any) {
	writeLogFile(
		filepath.Join(configDirPath(), "voice-agent.log"),
		fmt.Sprintf(format, args...),
		agentLogMaxBytes,
		1,
	)
}
