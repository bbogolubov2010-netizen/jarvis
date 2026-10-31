package dpapi

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	crypt32                = windows.NewLazySystemDLL("crypt32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procLocalFree          = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

const cryptprotectUiforbidden = 0x1

func Encrypt(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no data to encrypt")
	}
	in := &dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	out := &dataBlob{}
	ret, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(in)),
		0, 0, 0, 0,
		cryptprotectUiforbidden,
		uintptr(unsafe.Pointer(out)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("CryptProtectData failed: %w", err)
	}
	result := make([]byte, out.cbData)
	copy(result, unsafe.Slice(out.pbData, out.cbData))
	procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return result, nil
}

func Decrypt(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no data to decrypt")
	}
	in := &dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	out := &dataBlob{}
	ret, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(in)),
		0, 0, 0, 0,
		cryptprotectUiforbidden,
		uintptr(unsafe.Pointer(out)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("CryptUnprotectData failed: %w", err)
	}
	result := make([]byte, out.cbData)
	copy(result, unsafe.Slice(out.pbData, out.cbData))
	procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return result, nil
}
