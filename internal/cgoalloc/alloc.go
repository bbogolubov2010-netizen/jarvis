// Package cgoalloc provides small C-allocated buffers for cgo calls that
// cannot safely hold pointers into Go memory.
package cgoalloc

/*
#include <stdlib.h>
*/
import "C"

import "unsafe"

// CopyBytes copies b into C memory. The caller must release the returned
// pointer with Free.
func CopyBytes(b []byte) unsafe.Pointer {
	return C.CBytes(b)
}

// Free releases a pointer returned by CopyBytes.
func Free(p unsafe.Pointer) {
	C.free(p)
}
