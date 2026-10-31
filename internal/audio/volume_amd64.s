#include "textflag.h"

// func setMasterVolumeScalar(fn uintptr, iface unsafe.Pointer, level float32) uintptr
//
// Windows x64 ABI: RCX = this, XMM1 = the float argument (argument slot 2),
// R8 = the nullable event-context pointer (argument slot 3), and the caller
// provides 32 bytes of shadow space.
TEXT ·setMasterVolumeScalar(SB), NOSPLIT, $40-32
	MOVQ fn+0(FP), AX
	MOVQ iface+8(FP), CX
	MOVSS level+16(FP), X1
	XORQ R8, R8
	MOVQ R12, 32(SP)
	MOVQ SP, R12
	ANDQ $~15, SP
	SUBQ $32, SP
	CALL AX
	MOVQ R12, SP
	MOVQ 32(SP), R12
	MOVQ AX, ret+24(FP)
	RET


