//go:build windows

package voiceinput

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

// MicrophoneDiagnostics is a user-facing snapshot of microphone health. It
// combines endpoint enumeration, Windows privacy consent, device-manager
// states, and a short peak-level measurement.
type MicrophoneDiagnostics struct {
	Devices            []Device `json:"devices"`
	DeviceCount        int      `json:"device_count"`
	DefaultDevice      string   `json:"default_device"`
	DefaultDeviceID    string   `json:"default_device_id"`
	SelectedDevice     string   `json:"selected_device"`
	PrivacyAllowed     bool     `json:"privacy_allowed"`
	PrivacyValue       string   `json:"privacy_value"`
	PrivacySource      string   `json:"privacy_source"`
	ActiveEndpoints    int      `json:"active_endpoints"`
	DisabledEndpoints  int      `json:"disabled_endpoints"`
	UnpluggedEndpoints int      `json:"unplugged_endpoints"`
	Peak               float64  `json:"peak"`
	SignalDetected     bool     `json:"signal_detected"`
	Verdict            string   `json:"verdict"`
	Hints              []string `json:"hints"`
	Error              string   `json:"error"`
}

const (
	privacyConsentKey = `Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\microphone`
	mmDevicesKey      = `SOFTWARE\Microsoft\Windows\CurrentVersion\MMDevices\Audio\Capture`
)

// DiagnoseMicrophone inspects the selected microphone. An empty deviceID means
// the system default. The measurement records about 1.2 seconds of audio.
func DiagnoseMicrophone(deviceID string) MicrophoneDiagnostics {
	diag := MicrophoneDiagnostics{}
	devices, err := ListDevices()
	if err != nil {
		diag.Error = err.Error()
		diag.Verdict = "Не удалось перечислить микрофоны."
		diag.Hints = append(diag.Hints, "Проверьте, что служба Windows Audio запущена.")
		return diag
	}
	diag.Devices = devices
	diag.DeviceCount = len(devices)
	for _, device := range devices {
		if device.IsDefault {
			diag.DefaultDevice = device.Name
			diag.DefaultDeviceID = device.ID
		}
		if deviceID != "" && device.ID == deviceID {
			diag.SelectedDevice = device.Name
		}
	}
	if diag.SelectedDevice == "" {
		diag.SelectedDevice = diag.DefaultDevice
	}

	diag.PrivacyAllowed, diag.PrivacyValue, diag.PrivacySource = microphonePrivacy()
	diag.ActiveEndpoints, diag.DisabledEndpoints, diag.UnpluggedEndpoints = endpointStates()

	if len(devices) > 0 {
		diag.Peak = measurePeak(deviceID, 1200*time.Millisecond)
		diag.SignalDetected = diag.Peak >= 0.01
	}

	diag.Verdict, diag.Hints = diagnoseVerdict(diag)
	return diag
}

func microphonePrivacy() (allowed bool, value, source string) {
	keys := []struct {
		hive registry.Key
		path string
		name string
	}{
		{registry.CURRENT_USER, privacyConsentKey, "user"},
		{registry.LOCAL_MACHINE, privacyConsentKey, "machine"},
	}
	for _, key := range keys {
		open, err := registry.OpenKey(key.hive, key.path, registry.READ)
		if err != nil {
			continue
		}
		raw, _, err := open.GetStringValue("Value")
		open.Close()
		if err != nil || strings.TrimSpace(raw) == "" {
			continue
		}
		value = strings.TrimSpace(raw)
		source = key.name
		return !strings.EqualFold(value, "Deny"), value, source
	}
	// No explicit policy means Windows uses the default, which is allowed.
	return true, "unknown", ""
}

func endpointStates() (active, disabled, unplugged int) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, mmDevicesKey, registry.READ|registry.WOW64_64KEY)
	if err != nil {
		return 0, 0, 0
	}
	names, err := key.ReadSubKeyNames(-1)
	key.Close()
	if err != nil {
		return 0, 0, 0
	}
	for _, name := range names {
		sub, err := registry.OpenKey(registry.LOCAL_MACHINE, mmDevicesKey+`\`+name, registry.READ|registry.WOW64_64KEY)
		if err != nil {
			continue
		}
		state, _, err := sub.GetIntegerValue("DeviceState")
		sub.Close()
		if err != nil {
			continue
		}
		switch {
		case state&0x1 != 0:
			active++
		case state&0x2 != 0:
			disabled++
		case state&0x8 != 0:
			unplugged++
		}
	}
	return active, disabled, unplugged
}

func measurePeak(deviceID string, duration time.Duration) float64 {
	capture, err := StartCapture(deviceID)
	if err != nil {
		return 0
	}
	defer capture.Close()

	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	peak := 0.0
	for {
		select {
		case <-deadline.C:
			return peak
		case frame, ok := <-capture.Frames():
			if !ok {
				return peak
			}
			if level := normalizedRMS(frame); level > peak {
				peak = level
			}
		}
	}
}

func diagnoseVerdict(diag MicrophoneDiagnostics) (string, []string) {
	if diag.DeviceCount == 0 {
		return "Микрофон не найден.", []string{
			"Подключите микрофон или гарнитуру и нажмите «Проверить» ещё раз.",
			"Проверьте, что устройство видно в «Параметры → Система → Звук».",
		}
	}
	if !diag.PrivacyAllowed {
		return "Windows запрещает приложениям доступ к микрофону.", []string{
			"Откройте «Параметры → Конфиденциальность → Микрофон».",
			"Разрешите доступ приложениям и включите «Разрешить классическим приложениям доступ к микрофону».",
		}
	}
	if diag.ActiveEndpoints == 0 && diag.DisabledEndpoints > 0 {
		return "Микрофон отключён в настройках Windows.", []string{
			"Откройте «Параметры → Система → Звук → Все устройства ввода».",
			"Включите нужный микрофон и сделайте его устройством по умолчанию.",
		}
	}
	if diag.Peak < 0.002 {
		return "Микрофон подключён, но сигнал почти отсутствует.", []string{
			"Проверьте кнопку mute на гарнитуре или микрофоне.",
			"Убедитесь, что в «Параметры → Звук» выбран правильный вход.",
			"Поднимите уровень входного сигнала и скажите что-нибудь громко.",
		}
	}
	if diag.Peak < 0.01 {
		return "Микрофон работает, но уровень низкий.", []string{
			"Говорите ближе к микрофону или поднимите усиление входа.",
			"Попробуйте выбрать другое устройство в списке.",
		}
	}
	return "Микрофон работает, сигнал уверенный.", nil
}

// FormatDiagnostics renders a short human-readable summary for logs.
func FormatDiagnostics(diag MicrophoneDiagnostics) string {
	return fmt.Sprintf("mic: devices=%d default=%q peak=%.4f verdict=%q",
		diag.DeviceCount, diag.DefaultDevice, diag.Peak, diag.Verdict)
}
