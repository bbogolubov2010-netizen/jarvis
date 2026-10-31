#include "textflag.h"

// func callVoskRecognizerNew(fn, model uintptr, sampleRate float32) uintptr
//
// Windows x64 ABI: RCX = model, XMM1 = sample_rate. The caller provides
// 32 bytes of shadow space. Go's syscall.SyscallN cannot pass the float in
// XMM1, hence this tiny helper.
TEXT ·callVoskRecognizerNew(SB), NOSPLIT, $40-32
	MOVQ fn+0(FP), AX
	MOVQ model+8(FP), CX
	MOVSS sampleRate+16(FP), X1
	MOVQ R12, 32(SP)
	MOVQ SP, R12
	ANDQ $~15, SP
	SUBQ $32, SP
	CALL AX
	MOVQ R12, SP
	MOVQ 32(SP), R12
	MOVQ AX, ret+24(FP)
	RET

// func callVoskResult(fn, rec uintptr) *byte
//
// Windows x64 ABI: RCX = recognizer, return value is a char* in RAX.
TEXT ·callVoskResult(SB), NOSPLIT, $40-24
	MOVQ fn+0(FP), AX
	MOVQ rec+8(FP), CX
	MOVQ R12, 32(SP)
	MOVQ SP, R12
	ANDQ $~15, SP
	SUBQ $32, SP
	CALL AX
	MOVQ R12, SP
	MOVQ 32(SP), R12
	MOVQ AX, ret+16(FP)
	RET
