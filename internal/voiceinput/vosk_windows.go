//go:build windows

package voiceinput

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:noescape
func callVoskRecognizerNew(fn, model uintptr, sampleRate float32) uintptr

//go:noescape
func callVoskResult(fn, rec uintptr) *byte

type voskAPI struct {
	dll             *syscall.LazyDLL
	modelNew        *syscall.LazyProc
	modelFree       *syscall.LazyProc
	recognizerNew   *syscall.LazyProc
	recognizerFree  *syscall.LazyProc
	acceptWaveformS *syscall.LazyProc
	result          *syscall.LazyProc
	partialResult   *syscall.LazyProc
	finalResult     *syscall.LazyProc
	recognizerReset *syscall.LazyProc
	setGrammar      *syscall.LazyProc
}

var (
	voskLoadMu sync.Mutex
	voskLoaded *voskAPI
	voskErr    error
)

func loadVoskAPI(dllPath string) (*voskAPI, error) {
	voskLoadMu.Lock()
	defer voskLoadMu.Unlock()
	if voskLoaded != nil {
		return voskLoaded, nil
	}
	if voskErr != nil {
		return nil, voskErr
	}

	dir := filepath.Dir(dllPath)
	if ptr, err := windows.UTF16PtrFromString(dir); err == nil {
		proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("SetDllDirectoryW")
		_, _, _ = proc.Call(uintptr(unsafe.Pointer(ptr)))
		runtime.KeepAlive(ptr)
	}

	dll := syscall.NewLazyDLL(dllPath)
	if err := dll.Load(); err != nil {
		voskErr = fmt.Errorf("load libvosk.dll: %w", err)
		return nil, voskErr
	}
	api := &voskAPI{
		dll:             dll,
		modelNew:        dll.NewProc("vosk_model_new"),
		modelFree:       dll.NewProc("vosk_model_free"),
		recognizerNew:   dll.NewProc("vosk_recognizer_new"),
		recognizerFree:  dll.NewProc("vosk_recognizer_free"),
		acceptWaveformS: dll.NewProc("vosk_recognizer_accept_waveform_s"),
		result:          dll.NewProc("vosk_recognizer_result"),
		partialResult:   dll.NewProc("vosk_recognizer_partial_result"),
		finalResult:     dll.NewProc("vosk_recognizer_final_result"),
		recognizerReset: dll.NewProc("vosk_recognizer_reset"),
		setGrammar:      dll.NewProc("vosk_recognizer_set_grm"),
	}
	for name, proc := range map[string]*syscall.LazyProc{
		"vosk_model_new":                    api.modelNew,
		"vosk_model_free":                   api.modelFree,
		"vosk_recognizer_new":               api.recognizerNew,
		"vosk_recognizer_free":              api.recognizerFree,
		"vosk_recognizer_accept_waveform_s": api.acceptWaveformS,
		"vosk_recognizer_result":            api.result,
		"vosk_recognizer_partial_result":    api.partialResult,
		"vosk_recognizer_final_result":      api.finalResult,
		"vosk_recognizer_reset":             api.recognizerReset,
	} {
		if err := proc.Find(); err != nil {
			voskErr = fmt.Errorf("find %s: %w", name, err)
			return nil, voskErr
		}
	}
	voskLoaded = api
	return api, nil
}

// VoskModel owns a loaded Vosk acoustic model.
type VoskModel struct {
	api   *voskAPI
	model uintptr
	mu    sync.Mutex
}

// LoadVoskModel loads a Russian Vosk model directory.
func LoadVoskModel(dllPath, modelPath string) (*VoskModel, error) {
	api, err := loadVoskAPI(dllPath)
	if err != nil {
		return nil, err
	}
	modelPath = shortPath(modelPath)
	ptr, err := syscall.BytePtrFromString(modelPath)
	if err != nil {
		return nil, fmt.Errorf("encode Vosk model path: %w", err)
	}
	model, _, callErr := api.modelNew.Call(uintptr(unsafe.Pointer(ptr)))
	runtime.KeepAlive(ptr)
	if model == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return nil, fmt.Errorf("load Vosk model: %w", callErr)
		}
		return nil, fmt.Errorf("load Vosk model: %s", modelPath)
	}
	return &VoskModel{api: api, model: model}, nil
}

// NewRecognizer creates a 16 kHz mono PCM16 recognizer.
func (m *VoskModel) NewRecognizer() (*VoskRecognizer, error) {
	if m == nil || m.model == 0 {
		return nil, fmt.Errorf("Vosk model is not loaded")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := callVoskRecognizerNew(m.api.recognizerNew.Addr(), m.model, 16000)
	if rec == 0 {
		return nil, fmt.Errorf("create Vosk recognizer")
	}
	return &VoskRecognizer{api: m.api, rec: rec}, nil
}

// Close frees the model. Existing recognizers must be closed first.
func (m *VoskModel) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.model != 0 {
		m.api.modelFree.Call(m.model)
		m.model = 0
	}
}

// VoskRecognizer is a single streaming recognizer.
type VoskRecognizer struct {
	api *voskAPI
	rec uintptr
}

// Accept feeds PCM16 samples. It reports true when Vosk detects the end of an
// utterance.
func (r *VoskRecognizer) Accept(pcm []int16) (bool, error) {
	if r == nil || r.rec == 0 || len(pcm) == 0 {
		return false, nil
	}
	result, _, callErr := r.api.acceptWaveformS.Call(
		r.rec,
		uintptr(unsafe.Pointer(&pcm[0])),
		uintptr(len(pcm)),
	)
	runtime.KeepAlive(pcm)
	switch int32(result) {
	case 1:
		return true, nil
	case 0:
		return false, nil
	default:
		if callErr != nil && callErr != syscall.Errno(0) {
			return false, fmt.Errorf("Vosk accept waveform: %w", callErr)
		}
		return false, fmt.Errorf("Vosk accept waveform failed")
	}
}

// Result returns the current final result.
func (r *VoskRecognizer) Result() string {
	return r.readJSON(r.api.result)
}

// Partial returns the current partial result.
func (r *VoskRecognizer) Partial() string {
	return r.readJSON(r.api.partialResult)
}

// Final flushes and returns the final result.
func (r *VoskRecognizer) Final() string {
	return r.readJSON(r.api.finalResult)
}

// Reset clears the recognizer for the next utterance.
func (r *VoskRecognizer) Reset() {
	if r != nil && r.rec != 0 {
		r.api.recognizerReset.Call(r.rec)
	}
}

// SetGrammar restricts recognition to a JSON list of phrases. It is optional;
// the full small Russian model is used by default for better command coverage.
func (r *VoskRecognizer) SetGrammar(grammar string) {
	if r == nil || r.rec == 0 {
		return
	}
	ptr, err := syscall.BytePtrFromString(grammar)
	if err != nil {
		return
	}
	r.api.setGrammar.Call(r.rec, uintptr(unsafe.Pointer(ptr)))
	runtime.KeepAlive(ptr)
}

// Close frees the recognizer.
func (r *VoskRecognizer) Close() {
	if r == nil || r.rec == 0 {
		return
	}
	r.api.recognizerFree.Call(r.rec)
	r.rec = 0
}

func (r *VoskRecognizer) readJSON(proc *syscall.LazyProc) string {
	if r == nil || r.rec == 0 {
		return ""
	}
	ptr := callVoskResult(proc.Addr(), r.rec)
	if ptr == nil {
		return ""
	}
	raw := windows.BytePtrToString(ptr)
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return payload.Text
}

func shortPath(path string) string {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buf := make([]uint16, 512)
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetShortPathNameW")
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(ptr)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	runtime.KeepAlive(ptr)
	runtime.KeepAlive(buf)
	if n == 0 || n >= uintptr(len(buf)) {
		return path
	}
	return windows.UTF16ToString(buf[:n])
}
