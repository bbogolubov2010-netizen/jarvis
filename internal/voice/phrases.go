package voice

// Key identifies a spoken phrase. Keys are stable identifiers; the actual WAV
// files can be replaced without changing application code.
type Key string

const (
	KeyGreeting            Key = "greeting"
	KeyGreetingMorning     Key = "greeting_morning"
	KeyAck                 Key = "ack"
	KeyAsYouWish           Key = "as_you_wish"
	KeyAtYourService       Key = "at_your_service"
	KeyAlwaysAtYourService Key = "always_at_your_service"
	KeyLoading             Key = "loading"
	KeyRequestDone         Key = "request_done"
	KeyCheckDone           Key = "check_done"
	KeyDiagnosticsStart    Key = "diagnostics_start"
	KeySetupStart          Key = "setup_start"
	KeySetupImport         Key = "setup_import"
	KeySetupReady          Key = "setup_ready"
	KeyShutdown            Key = "shutdown"
	KeyRestart             Key = "restart"
	KeyRebooted            Key = "rebooted"
	KeyScreenshotConfirm   Key = "screenshot_confirm"
	KeyScanDone            Key = "scan_done"
	KeyImageStart          Key = "image_start"
	KeyImageDone           Key = "image_done"
	KeyImageSaved          Key = "image_saved"
	KeyImageFail           Key = "image_fail"
	KeySearchStart         Key = "search_start"
	KeyNoInfo              Key = "no_info"
	KeyCreated             Key = "created"
	KeyError               Key = "error"
	KeyUnclear             Key = "unclear"
	KeyBatteryLow          Key = "battery_low"
	KeyPowerReserve        Key = "power_reserve"
)

// phraseFiles maps a phrase key to one or more WAV files. When several files
// are listed, the player picks one at random so repeated acknowledgements do
// not sound identical.
var phraseFiles = map[Key][]string{
	KeyGreeting:            {"greeting.wav"},
	KeyGreetingMorning:     {"greeting_morning.wav"},
	KeyAck:                 {"yes.wav", "yes_alt.wav", "roger.wav"},
	KeyAsYouWish:           {"as_you_wish.wav"},
	KeyAtYourService:       {"at_your_service.wav"},
	KeyAlwaysAtYourService: {"always_at_your_service.wav"},
	KeyLoading:             {"loading.wav"},
	KeyRequestDone:         {"request_done.wav"},
	KeyCheckDone:           {"check_done.wav"},
	KeyDiagnosticsStart:    {"diagnostics_start.wav", "diagnostics_start_alt.wav"},
	KeySetupStart:          {"setup_start.wav"},
	KeySetupImport:         {"setup_import.wav"},
	KeySetupReady:          {"setup_ready.wav"},
	KeyShutdown:            {"shutdown.wav"},
	KeyRestart:             {"restart.wav"},
	KeyRebooted:            {"rebooted.wav"},
	KeyScreenshotConfirm:   {"screenshot_confirm.wav"},
	KeyScanDone:            {"scan_done.wav"},
	KeyImageStart:          {"image_start.wav"},
	KeyImageDone:           {"image_done.wav", "image_done_alt.wav"},
	KeyImageSaved:          {"image_saved.wav"},
	KeyImageFail:           {"image_fail.wav"},
	KeySearchStart:         {"search_start.wav"},
	KeyNoInfo:              {"no_info.wav"},
	KeyCreated:             {"created.wav"},
	KeyError:               {"error.wav"},
	KeyUnclear:             {"unclear.wav"},
	KeyBatteryLow:          {"battery_low.wav"},
	KeyPowerReserve:        {"power_reserve.wav"},
}

// FilesFor returns the WAV file names for a key. The returned slice is a copy
// so callers cannot mutate the phrase table.
func FilesFor(key Key) []string {
	files := phraseFiles[key]
	if len(files) == 0 {
		return nil
	}
	return append([]string(nil), files...)
}

// AllFiles returns every unique WAV file name used by the phrase table.
func AllFiles() []string {
	seen := make(map[string]struct{}, len(phraseFiles))
	files := make([]string, 0, len(phraseFiles))
	for _, names := range phraseFiles {
		for _, name := range names {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			files = append(files, name)
		}
	}
	return files
}

// Keys returns every known phrase key. It is mainly useful for tests and
// diagnostics.
func Keys() []Key {
	keys := make([]Key, 0, len(phraseFiles))
	for key := range phraseFiles {
		keys = append(keys, key)
	}
	return keys
}
