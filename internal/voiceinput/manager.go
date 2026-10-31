//go:build windows

package voiceinput

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"Jarvis/internal/intent"
	"Jarvis/internal/voiceengine"
)

// State is the user-visible voice pipeline state.
type State string

var errStartCanceled = errors.New("voice start canceled")

const (
	StateStopped    State = "stopped"
	StateStarting   State = "starting"
	StateWake       State = "wake"
	StateCommand    State = "command"
	StateProcessing State = "processing"
	StateSpeaking   State = "speaking"
	StateError      State = "error"
)

// Callbacks are invoked from the voice goroutines. OnCommand may block while
// the assistant speaks; it should return true to keep listening for a follow-up
// (for example a confirmation answer).
type Callbacks struct {
	OnState      func(State)
	OnLevel      func(float64)
	OnWake       func(rest string)
	OnCommand    func(string) bool
	OnTranscript func(string)
	OnVosk       func(string)
	OnError      func(string)
}

// Status is a serializable snapshot for the UI.
type Status struct {
	State            State  `json:"state"`
	WakeAvailable    bool   `json:"wake_available"`
	WakeAvailableEn  bool   `json:"wake_available_en"`
	WhisperAvailable bool   `json:"whisper_available"`
	Language         string `json:"language"`
	DeviceID         string `json:"device_id"`
	LastTranscript   string `json:"last_transcript"`
	LastError        string `json:"last_error"`
}

// Manager owns the microphone, VAD, Vosk wake detector and Whisper command
// transcriber. Only one manager should listen on a machine at a time.
type Manager struct {
	paths            voiceengine.Paths
	deviceID         string
	wakeWord         string
	wakeWordDisabled bool
	language         string
	callbacks        Callbacks

	mu              sync.Mutex
	state           State
	started         bool
	stopping        bool
	startDone       chan struct{}
	paused          bool
	awaiting        bool
	awaitingUntil   time.Time
	lastTranscript  string
	lastError       string
	wakeAvailable   bool
	wakeAvailableEn bool
	whisperReady    bool

	ctx       context.Context
	cancel    context.CancelFunc
	capture   *Capture
	vosk      *VoskModel
	voskRec   *VoskRecognizer
	voskEn    *VoskModel
	voskRecEn *VoskRecognizer
	whisper   *Transcriber
	commands  chan []int16
	wg        sync.WaitGroup
	levelTick time.Time
}

// NewManager creates a manager for the supplied engine paths.
func NewManager(paths voiceengine.Paths, deviceID string, callbacks Callbacks) *Manager {
	return &Manager{
		paths:     paths,
		deviceID:  deviceID,
		callbacks: callbacks,
		state:     StateStopped,
		commands:  make(chan []int16, 2),
	}
}

// SetWakeWord overrides the phrase stripped from a recognized wake utterance.
// The built-in Russian variants remain available as a fallback.
func (m *Manager) SetWakeWord(word string) {
	m.mu.Lock()
	m.wakeWord = strings.TrimSpace(word)
	m.mu.Unlock()
}

// SetWakeWordDisabled keeps the microphone available for push-to-talk while
// skipping Vosk wake-word detection.
func (m *Manager) SetWakeWordDisabled(disabled bool) {
	m.mu.Lock()
	m.wakeWordDisabled = disabled
	m.mu.Unlock()
}

// SetLanguage selects the Whisper transcription language: auto, ru, or en.
func (m *Manager) SetLanguage(language string) {
	m.mu.Lock()
	m.language = strings.ToLower(strings.TrimSpace(language))
	m.mu.Unlock()
}

func (m *Manager) stopRequested() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopping
}

// abortStart releases models loaded before the microphone was opened. The
// caller must not hold m.mu.
func (m *Manager) abortStart(cancel context.CancelFunc, capture *Capture) error {
	m.mu.Lock()
	vosk := m.vosk
	rec := m.voskRec
	voskEn := m.voskEn
	recEn := m.voskRecEn
	m.vosk = nil
	m.voskRec = nil
	m.voskEn = nil
	m.voskRecEn = nil
	m.capture = nil
	m.wakeAvailable = false
	m.wakeAvailableEn = false
	m.started = false
	m.stopping = false
	m.state = StateStopped
	m.mu.Unlock()
	if capture != nil {
		_ = capture.Close()
	}
	if rec != nil {
		rec.Close()
	}
	if vosk != nil {
		vosk.Close()
	}
	if recEn != nil {
		recEn.Close()
	}
	if voskEn != nil {
		voskEn.Close()
	}
	if cancel != nil {
		cancel()
	}
	return errStartCanceled
}

// Start opens the microphone and begins wake-word detection. It is safe to
// call from a goroutine because loading the Vosk model can take a moment.
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	m.stopping = false
	m.state = StateStarting
	m.lastError = ""
	// A stopped manager may still hold a queued utterance from its previous
	// run. Start with a fresh queue so stale commands cannot execute twice.
	m.commands = make(chan []int16, 2)
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel
	m.startDone = make(chan struct{})
	startDone := m.startDone
	wakeDisabled := m.wakeWordDisabled
	m.mu.Unlock()
	defer close(startDone)
	m.emitState(StateStarting)

	var wakeErr error
	if !wakeDisabled {
		// Russian is the primary wake language. The English model is optional
		// and runs alongside it, so "hey Jarvis" works even in a Russian
		// session and vice versa.
		ruReady := m.paths.VoskReady()
		if ruReady {
			model, err := LoadVoskModel(m.paths.VoskDLL, m.paths.VoskModel)
			if err != nil {
				wakeErr = err
			} else if rec, err := model.NewRecognizer(); err != nil {
				model.Close()
				wakeErr = err
			} else {
				m.mu.Lock()
				m.vosk = model
				m.voskRec = rec
				m.wakeAvailable = true
				m.mu.Unlock()
			}
		}
		if m.paths.VoskReadyEn() {
			model, err := LoadVoskModel(m.paths.VoskDLL, m.paths.VoskModelEn)
			if err != nil {
				if wakeErr == nil {
					wakeErr = err
				}
			} else if rec, err := model.NewRecognizer(); err != nil {
				model.Close()
				if wakeErr == nil {
					wakeErr = err
				}
			} else {
				m.mu.Lock()
				m.voskEn = model
				m.voskRecEn = rec
				m.wakeAvailableEn = true
				m.mu.Unlock()
				wakeErr = nil
			}
		}
	}
	m.mu.Lock()
	anyWake := m.wakeAvailable || m.wakeAvailableEn
	m.mu.Unlock()
	if wakeErr != nil && !anyWake {
		m.setError(fmt.Sprintf("Wake word unavailable: %v", wakeErr))
	}
	if m.stopRequested() {
		return m.abortStart(cancel, nil)
	}

	capture, err := StartCapture(m.deviceID)
	if err != nil {
		m.setError(err.Error())
		m.mu.Lock()
		m.state = StateError
		m.started = false
		vosk := m.vosk
		rec := m.voskRec
		voskEn := m.voskEn
		recEn := m.voskRecEn
		m.vosk = nil
		m.voskRec = nil
		m.voskEn = nil
		m.voskRecEn = nil
		m.wakeAvailable = false
		m.wakeAvailableEn = false
		m.mu.Unlock()
		if rec != nil {
			rec.Close()
		}
		if vosk != nil {
			vosk.Close()
		}
		if recEn != nil {
			recEn.Close()
		}
		if voskEn != nil {
			voskEn.Close()
		}
		cancel()
		return err
	}
	if m.stopRequested() {
		return m.abortStart(cancel, capture)
	}
	m.mu.Lock()
	m.capture = capture
	m.whisperReady = m.paths.WhisperReady()
	if m.whisperReady {
		m.whisper = NewTranscriber(m.paths.WhisperCLI, m.paths.WhisperModel)
		m.whisper.Language = m.language
	}
	m.mu.Unlock()

	m.wg.Add(2)
	go m.audioLoop(capture)
	go m.commandWorker()

	m.setState(StateWake)
	return nil
}

// Stop closes the microphone and releases models.
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.stopping = true
	cancel := m.cancel
	startDone := m.startDone
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// Start may still be loading Vosk or opening the microphone. Wait for it
	// to observe stopping and clean up, or to finish starting, before closing
	// the capture and models.
	if startDone != nil {
		<-startDone
	}

	m.mu.Lock()
	m.started = false
	capture := m.capture
	m.capture = nil
	m.mu.Unlock()
	if capture != nil {
		_ = capture.Close()
	}
	m.wg.Wait()

	m.mu.Lock()
	if m.voskRec != nil {
		m.voskRec.Close()
		m.voskRec = nil
	}
	if m.vosk != nil {
		m.vosk.Close()
		m.vosk = nil
	}
	if m.voskRecEn != nil {
		m.voskRecEn.Close()
		m.voskRecEn = nil
	}
	if m.voskEn != nil {
		m.voskEn.Close()
		m.voskEn = nil
	}
	m.whisper = nil
	m.wakeAvailable = false
	m.wakeAvailableEn = false
	m.whisperReady = false
	m.awaiting = false
	m.paused = false
	m.stopping = false
	m.state = StateStopped
	m.mu.Unlock()
	m.emitState(StateStopped)
}

// PushToTalk makes the next utterance a command without a wake word.
func (m *Manager) PushToTalk() bool {
	m.mu.Lock()
	if !m.started || m.state == StateProcessing {
		m.mu.Unlock()
		return false
	}
	m.awaiting = true
	m.awaitingUntil = time.Now().Add(12 * time.Second)
	m.paused = false
	m.mu.Unlock()
	m.setState(StateCommand)
	return true
}

// Pause gates microphone processing while JARVIS is speaking.
func (m *Manager) Pause() {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.paused = true
	m.mu.Unlock()
	m.setState(StateSpeaking)
}

// Resume re-enables microphone processing.
func (m *Manager) Resume() {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.paused = false
	awaiting := m.awaiting
	state := StateWake
	if awaiting {
		state = StateCommand
	}
	m.mu.Unlock()
	m.setState(state)
}

// Status returns a snapshot for the settings UI.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	language := m.language
	if language == "" {
		language = "auto"
	}
	return Status{
		State:            m.state,
		WakeAvailable:    m.wakeAvailable,
		WakeAvailableEn:  m.wakeAvailableEn,
		WhisperAvailable: m.whisperReady,
		Language:         language,
		DeviceID:         m.deviceID,
		LastTranscript:   m.lastTranscript,
		LastError:        m.lastError,
	}
}

func (m *Manager) audioLoop(capture *Capture) {
	defer m.wg.Done()
	segmenter := NewSegmenter()
	frames := capture.Frames()

	for {
		select {
		case <-m.ctx.Done():
			return
		case frame, ok := <-frames:
			if !ok {
				return
			}
			if m.isPaused() {
				segmenter.Reset()
				continue
			}

			level := normalizedRMS(frame)
			if time.Since(m.levelTick) > 100*time.Millisecond {
				m.levelTick = time.Now()
				if m.callbacks.OnLevel != nil {
					m.callbacks.OnLevel(level)
				}
			}

			for _, utterance := range segmenter.Feed(frame) {
				m.handleUtterance(utterance, segmenter)
			}

			m.expireAwaiting()
		}
	}
}

func (m *Manager) handleUtterance(utterance []int16, segmenter *Segmenter) {
	m.mu.Lock()
	awaiting := m.awaiting
	state := m.state
	m.mu.Unlock()

	if awaiting || state == StateCommand {
		m.mu.Lock()
		m.awaiting = false
		m.mu.Unlock()
		m.setState(StateProcessing)
		select {
		case m.commands <- utterance:
		default:
			m.setError("Command queue is full")
			m.setState(StateWake)
		}
		return
	}

	text := m.recognizeVosk(utterance)
	if text == "" {
		return
	}
	if m.callbacks.OnVosk != nil {
		m.callbacks.OnVosk(text)
	}
	rest, hadWake := m.wakeText(text)
	if !hadWake {
		return
	}
	rest = strings.TrimSpace(rest)
	for _, filler := range []string{"сэр", "пожалуйста", "sir", "please"} {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, filler))
	}
	rest = strings.TrimSpace(rest)

	// The acknowledgement is synchronous so the microphone does not record
	// JARVIS's own voice. Buffered frames from the acknowledgement are dropped.
	m.mu.Lock()
	m.paused = true
	m.mu.Unlock()
	if m.callbacks.OnWake != nil {
		m.callbacks.OnWake(rest)
	}
	m.drainCapture()
	segmenter.Reset()
	m.mu.Lock()
	m.paused = false
	m.mu.Unlock()

	if rest == "" {
		m.mu.Lock()
		m.awaiting = true
		m.awaitingUntil = time.Now().Add(9 * time.Second)
		m.mu.Unlock()
		m.setState(StateCommand)
		return
	}

	m.setState(StateProcessing)
	select {
	case m.commands <- utterance:
	default:
		m.setError("Command queue is full")
		m.setState(StateWake)
	}
}

func (m *Manager) drainCapture() {
	m.mu.Lock()
	capture := m.capture
	m.mu.Unlock()
	if capture == nil {
		return
	}
	for {
		select {
		case <-capture.Frames():
		default:
			return
		}
	}
}

func (m *Manager) commandWorker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case utterance := <-m.commands:
			m.processCommand(utterance)
		}
	}
}

func (m *Manager) processCommand(utterance []int16) {
	m.mu.Lock()
	whisper := m.whisper
	m.mu.Unlock()
	if whisper == nil || !whisper.Available() {
		m.setError("Whisper engine is not installed")
		m.setState(StateWake)
		return
	}

	ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
	text, err := whisper.Transcribe(ctx, utterance)
	cancel()
	if err != nil {
		m.setError(err.Error())
		m.setState(StateWake)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		m.setError("Не расслышал команду, сэр.")
		m.setState(StateWake)
		return
	}

	m.mu.Lock()
	m.lastTranscript = text
	m.mu.Unlock()
	if m.callbacks.OnTranscript != nil {
		m.callbacks.OnTranscript(text)
	}

	followUp := false
	if m.callbacks.OnCommand != nil {
		followUp = m.callbacks.OnCommand(text)
	}
	if followUp {
		m.mu.Lock()
		m.awaiting = true
		m.awaitingUntil = time.Now().Add(15 * time.Second)
		m.mu.Unlock()
		m.setState(StateCommand)
		return
	}
	m.setState(StateWake)
}

func (m *Manager) recognizeVosk(utterance []int16) string {
	m.mu.Lock()
	rec := m.voskRec
	recEn := m.voskRecEn
	m.mu.Unlock()

	russian := recognizeWith(rec, utterance)
	if russian != "" {
		if _, had := m.wakeText(russian); had {
			return russian
		}
	}
	english := recognizeWith(recEn, utterance)
	if english != "" {
		if _, had := m.wakeText(english); had {
			return english
		}
		if russian == "" {
			return english
		}
	}
	return russian
}

func recognizeWith(rec *VoskRecognizer, utterance []int16) string {
	if rec == nil {
		return ""
	}
	rec.Reset()
	const chunk = 4000
	for start := 0; start < len(utterance); start += chunk {
		end := start + chunk
		if end > len(utterance) {
			end = len(utterance)
		}
		_, _ = rec.Accept(utterance[start:end])
	}
	text := rec.Final()
	rec.Reset()
	return text
}

func (m *Manager) expireAwaiting() {
	m.mu.Lock()
	expired := m.awaiting && time.Now().After(m.awaitingUntil)
	if expired {
		m.awaiting = false
	}
	m.mu.Unlock()
	if expired {
		m.setState(StateWake)
	}
}

func (m *Manager) isPaused() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused
}

func (m *Manager) setState(state State) {
	m.mu.Lock()
	if m.state == state {
		m.mu.Unlock()
		return
	}
	m.state = state
	m.mu.Unlock()
	m.emitState(state)
}

func (m *Manager) emitState(state State) {
	if m.callbacks.OnState != nil {
		m.callbacks.OnState(state)
	}
}

func (m *Manager) setError(message string) {
	m.mu.Lock()
	m.lastError = message
	m.mu.Unlock()
	if m.callbacks.OnError != nil {
		m.callbacks.OnError(message)
	}
}

func (m *Manager) wakeText(text string) (string, bool) {
	m.mu.Lock()
	custom := m.wakeWord
	m.mu.Unlock()
	if custom != "" {
		normalized := intent.NormalizeRussian(text)
		phrase := intent.NormalizeRussian(custom)
		if phrase != "" {
			if normalized == phrase {
				return "", true
			}
			if strings.HasPrefix(normalized, phrase+" ") {
				return strings.TrimSpace(normalized[len(phrase):]), true
			}
		}
	}
	return wakeText(text)
}

func wakeText(text string) (string, bool) {
	rest, had := intent.StripWakeWord(text)
	if had {
		return rest, true
	}
	normalized := intent.NormalizeRussian(text)
	for _, name := range []string{"джарвис", "жарвис", "дарвис", "jarvis"} {
		if index := strings.Index(normalized, name); index >= 0 {
			return strings.TrimSpace(normalized[index+len(name):]), true
		}
	}
	return normalized, false
}
