//go:build manual

package audio

import (
	"testing"
	"unsafe"
)

func TestManualScalarCall(t *testing.T) {
	err := withEndpointVolume(func(volume unsafe.Pointer) error {
		vtable := *(*unsafe.Pointer)(volume)
		setFn := *(*uintptr)(unsafe.Add(vtable, 7*unsafe.Sizeof(uintptr(0))))
		hr := setMasterVolumeScalar(setFn, volume, 0.25)
		t.Logf("set hr=0x%x", uint32(hr))
		level := float32(0.42)
		hr = call(volume, 9, uintptr(unsafe.Pointer(&level)))
		t.Logf("get hr=0x%x level=%f", uint32(hr), level)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestManualVolume(t *testing.T) {
	level, err := GetVolumePercent()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("volume=%d", level)
	if level < 0 || level > 100 {
		t.Fatalf("invalid volume %d", level)
	}
	if err := SetVolumePercent(25); err != nil {
		t.Fatal(err)
	}
	updated, err := GetVolumePercent()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("updated=%d", updated)
	if updated < 20 || updated > 30 {
		t.Fatalf("set/get mismatch: %d", updated)
	}
	if err := SetVolumePercent(level); err != nil {
		t.Fatal(err)
	}
}
