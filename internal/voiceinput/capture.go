//go:build windows

package voiceinput

import (
	"encoding/binary"
	"fmt"
	"sync"

	"Jarvis/internal/cgoalloc"

	"github.com/gen2brain/malgo"
)

// Device describes an audio endpoint.
type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	id        malgo.DeviceID
}

// ListDevices returns capture devices in the order reported by miniaudio.
func ListDevices() ([]Device, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("init audio context: %w", err)
	}
	defer ctx.Free()

	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, fmt.Errorf("enumerate capture devices: %w", err)
	}
	devices := make([]Device, 0, len(infos))
	for _, info := range infos {
		devices = append(devices, Device{
			ID:        info.ID.String(),
			Name:      info.Name(),
			IsDefault: info.IsDefault != 0,
			id:        info.ID,
		})
	}
	return devices, nil
}

// Capture is a 16 kHz mono PCM16 microphone stream.
type Capture struct {
	ctx    *malgo.AllocatedContext
	device *malgo.Device
	frames chan []int16

	closeOnce sync.Once
	closeErr  error
}

// StartCapture opens the selected device. An empty deviceID selects the
// system default. The returned channel receives 30 ms PCM16 frames.
func StartCapture(deviceID string) (*Capture, error) {
	devices, err := ListDevices()
	if err != nil {
		return nil, err
	}
	var selected *Device
	for i := range devices {
		if devices[i].ID == deviceID {
			selected = &devices[i]
			break
		}
	}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("init audio context: %w", err)
	}

	config := malgo.DefaultDeviceConfig(malgo.Capture)
	config.SampleRate = 16000
	config.PerformanceProfile = malgo.LowLatency
	config.PeriodSizeInMilliseconds = 30
	config.Capture.Format = malgo.FormatS16
	config.Capture.Channels = 1
	config.Capture.ShareMode = malgo.Shared

	c := &Capture{
		ctx:    ctx,
		frames: make(chan []int16, 64),
	}
	if selected != nil {
		// miniaudio copies the device ID into its own structure during
		// initialization, but cgo rejects a Go pointer embedded in the config.
		// Allocate the ID in C memory and release it when StartCapture returns.
		deviceID := cgoalloc.CopyBytes(selected.id[:])
		defer cgoalloc.Free(deviceID)
		config.Capture.DeviceID = deviceID
	}

	callbacks := malgo.DeviceCallbacks{
		Data: func(_, input []byte, _ uint32) {
			if len(input) < 2 {
				return
			}
			samples := make([]int16, len(input)/2)
			for i := range samples {
				samples[i] = int16(binary.LittleEndian.Uint16(input[i*2:]))
			}
			select {
			case c.frames <- samples:
			default:
				// Drop a frame instead of blocking miniaudio's realtime thread.
			}
		},
	}

	device, err := malgo.InitDevice(ctx.Context, config, callbacks)
	if err != nil {
		ctx.Free()
		return nil, fmt.Errorf("open microphone: %w", err)
	}
	c.device = device

	if err := device.Start(); err != nil {
		device.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("start microphone: %w", err)
	}
	return c, nil
}

// Frames returns the live PCM16 stream.
func (c *Capture) Frames() <-chan []int16 {
	if c == nil {
		return nil
	}
	return c.frames
}

// Close stops capture and releases the device.
func (c *Capture) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.device != nil {
			c.closeErr = c.device.Stop()
			c.device.Uninit()
			c.device = nil
		}
		if c.ctx != nil {
			c.ctx.Free()
			c.ctx = nil
		}
		close(c.frames)
	})
	return c.closeErr
}
