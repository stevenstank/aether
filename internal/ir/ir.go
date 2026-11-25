// Package ir defines Aether's intermediate representation (D-004, D-021).
//
// The IR is Aether's own: it imports no go/* package, and lowering is the
// only stage that translates from the type-checked Go syntax into it. Its
// meaning is specified here and in docs/ARCHITECTURE.md §4, precisely enough
// that a backend or interpreter can consume it without consulting Go source.
//
// The instruction set covers subset S1 (docs/SEMANTICS.md), the calls of
// D-023 increment 4.2, the control flow and bool of increment 4.1, the
// multiple results of increment 4.3, the sized integer types, conversions,
// bit operations, and print of increment 4.4, the division, shifts, and
// panics of increment 4.5, and the package-level variables and package
// initialization of increment 4.6. A program is a set of functions and
// global variables; a function owns its local variable slots
// and a list of basic blocks; a block is a sequence of instructions ending in
// one terminator. An instruction that produces a value is that value:
// operands refer to such instructions directly.
package ir

import (
	"fmt"
	"math"
)

// Type is the type of a value or slot.
type Type uint8

const (
	// NoType is the type of instructions that produce no value.
	NoType Type = iota
	// I64 is a 64-bit two's-complement integer. Go's int lowers to I64
	// (types.SizesFor("gc", "amd64")).
	I64
	// Bool is a boolean, held as 0 (false) or 1 (true).
	Bool
	// Tuple is the type of a call of a function with two or more results,
	// whose types are the callee's Results. Only Extract uses a tuple.
	Tuple
	// I8, I16, and I32 are signed and U8, U16, U32, and U64 unsigned
	// integers of their width. Every integer value is held in 64 bits in
	// canonical form (D-023): sign-extended from its width if signed,
	// zero-extended if unsigned.
	I8
	I16
	I32
	U8
	U16
	U32
	U64
	// F64 and F32 are IEEE 754 binary64 and binary32 floats (D-032), held
	// in 64 bits as their bit patterns: an F32's 32 bits zero-extended.
	F64
	F32
	// Ptr is the address of a program string's bytes (D-033), or of a
	// byte of a memory object (D-034) or of a heap object (D-035), or 0
	// (nil). Lowering represents a Go string as a Ptr and an I64 length, an
	// array or struct value as the address of the memory holding it, and a
	// Go pointer as a Ptr. Str, Addr, Alloc, PAdd, and the constant 0
	// produce one; LoadB reads through a string's, and LdM and StM through
	// an object's. A string's bytes are a program string's or a heap
	// object's (D-037), which LoadB, LdM, PAdd, and Move's source accept
	// alike; a program string's are never written. Eq and Ne compare two
	// Ptrs as addresses.
	Ptr
)

var typeNames = [...]string{
	NoType: "none", I64: "i64", Bool: "bool", Tuple: "tuple",
	I8: "i8", I16: "i16", I32: "i32", U8: "u8", U16: "u16", U32: "u32", U64: "u64",
	F64: "f64", F32: "f32", Ptr: "ptr",
}

func (t Type) String() string {
	if int(t) < len(typeNames) && typeNames[t] != "" {
		return typeNames[t]
	}
	return fmt.Sprintf("Type(%d)", uint8(t))
}

// MemSize returns the bytes a value of type t occupies in memory (D-034):
// 1 for a Bool, I8, or U8, 2 for I16 and U16, 4 for I32, U32, and F32, and
// 8 for I64, U64, F64, and Ptr; 0 for the others, which have no memory form.
func (t Type) MemSize() int64 {
	switch t {
	case Bool, I8, U8:
		return 1
	case I16, U16:
		return 2
	case I32, U32, F32:
		return 4
	case I64, U64, F64, Ptr:
		return 8
	}
	return 0
}

// IsInteger reports whether t is one of the integer types.
func (t Type) IsInteger() bool { return t == I64 || t >= I8 && t <= U64 }

// IsFloat reports whether t is F64 or F32.
func (t Type) IsFloat() bool { return t == F64 || t == F32 }

// Signed reports whether t is a signed integer type.
func (t Type) Signed() bool { return t == I64 || t >= I8 && t <= I32 }

// Width returns the width of an integer type in bits.
func (t Type) Width() int {
	switch t {
	case I8, U8:
		return 8
	case I16, U16:
		return 16
	case I32, U32:
		return 32
	}
	return 64
}

// Canonical returns the canonical form of v reduced modulo 2^width for
// the integer type t: its low width bits, sign-extended if t is signed and
// zero-extended if not. For an F32 it returns the low 32 bits
// zero-extended, and for a Bool, I64, or F64 v unchanged.
func (t Type) Canonical(v int64) int64 {
	if t == F32 {
		return int64(uint32(v))
	}
	w := t.Width()
	if !t.IsInteger() || w == 64 {
		return v
	}
	if t.Signed() {
		return v << (64 - w) >> (64 - w)
	}
	return int64(uint64(v) << (64 - w) >> (64 - w))
}

// Op is an instruction's operation. The meaning of each operation is given
// with its constant; "args" are the instruction's value operands, in order.
type Op uint8

const (
	invalidOp Op = iota
	// Const produces the constant Const of its type, in canonical form:
	// 0 or 1 for Bool. No args.
	Const
	// Load produces the current contents of Slot. No args. The slot must
	// have been stored to earlier on every path to the load.
	Load
	// Store writes args[0] to Slot. It produces no value.
	Store
	// Add, Sub, and Mul produce args[0] op args[1], whose integer type
	// they share, wrapping at its width: the canonical form of the exact
	// result reduced modulo 2^width. They never trap.
	Add
	Sub
	Mul
	// Neg produces 0 - args[0], wrapping; Neg of a signed type's minimum
	// is that minimum.
	Neg
	// And, Or, Xor, and AndNot (x &^ y) produce the bitwise result of
	// args[0] and args[1], whose integer type they share; Com (unary ^)
	// produces the complement of args[0]. All are in canonical form.
	And
	Or
	Xor
	AndNot
	Com
	// Div and Rem produce the quotient, truncated toward zero, and the
	// remainder of args[0] by args[1], whose integer type they share. The
	// minimum of a signed type divided by -1 is that minimum, with
	// remainder 0. Precondition: args[1] is not zero (lowering checks it
	// first, D-023).
	Div
	Rem
	// Shl and Shr shift args[0], of any integer type, left or right by
	// args[1], of any unsigned type. A count at or above the width gives
	// 0, or for Shr of a signed type the sign fill (0 or -1); Shr of a
	// signed type is arithmetic.
	Shl
	Shr
	// Panic ends the program with the run-time error Index, one of the
	// Panic constants: the message and exit status 2. It is a terminator.
	// PanicIndex and the slice bounds errors (IsBounds) have args: x, of any
	// integer type, and y, an I64 (BoundsMessage); PanicMsg and PanicFatal have their
	// message's Ptr and I64 length; the others have none.
	Panic
	// Conv produces args[0], of any integer type, converted to the
	// instruction's integer type: the canonical form of its value reduced
	// modulo 2^width of the new type.
	Conv
	// Print writes its args as Println does, but with no separators and no
	// trailing newline.
	Print
	// Println writes its args to standard error, signed types in signed
	// decimal, unsigned types in unsigned decimal, and Bool as true or
	// false, separated by single spaces and followed by a newline, in that
	// order.
	// The args are already-evaluated values, so all of them are computed
	// before any output. Write errors are ignored (D-009). It produces no
	// value.
	Println
	// Ret ends the function, returning its args as the function's
	// results: one arg per entry of Results, of the same type. It is a
	// terminator and produces no value.
	Ret
	// Jump continues at Targets[0]. It is a terminator. No args.
	Jump
	// Branch continues at Targets[0] if args[0], a Bool, is true, and at
	// Targets[1] otherwise. It is a terminator.
	Branch
	// Eq, Ne, Lt, Le, Gt, and Ge produce the Bool result of comparing
	// args[0] with args[1], which have the same type, as signed or
	// unsigned numbers by that type; only Eq and Ne for Bool.
	Eq
	Ne
	Lt
	Le
	Gt
	Ge
	// Not produces the negation of args[0], a Bool.
	Not
	// Extract produces result Index of args[0], a Tuple call in the same
	// block.
	Extract
	// Param produces the value of parameter Index of the function. No
	// args. It appears only in the entry block.
	Param
	// Call calls Callee, a function of the program, with args, one per
	// entry of Callee.Params and of the same type, and produces its result:
	// the type is Callee's single result type, NoType if it has none, or
	// Tuple if it has several.
	// Each call has a fresh set of slots.
	Call
	// GLoad produces the current contents of Global. No args. Globals start
	// at zero, so a GLoad needs no earlier GStore.
	GLoad
	// GStore writes args[0] to Global. It produces no value.
	GStore
	// Phi, only in a function in SSA form (D-025), produces args[i] when
	// control reached its block along Edges[i]. A block's phis come before
	// its other instructions and take their values together, on entry to
	// the block.
	Phi
	// FAdd, FSub, FMul, and FDiv produce the IEEE 754 result of args[0] op
	// args[1], whose float type they share, rounded to nearest, ties to
	// even, at that type's precision; each is rounded on its own, never
	// fused (D-032). FNeg flips the sign bit of args[0].
	FAdd
	FSub
	FMul
	FDiv
	FNeg
	// FEq, FNe, FLt, FLe, FGt, and FGe produce the Bool result of the IEEE
	// 754 comparison of args[0] with args[1], which have the same float
	// type: a NaN is unordered, so only FNe is true for it, and -0 equals
	// +0.
	FEq
	FNe
	FLt
	FLe
	FGt
	FGe
	// IToF produces args[0], of any integer type, converted to the
	// instruction's float type, rounded to nearest, ties to even.
	IToF
	// FToF produces args[0], of one float type, converted to the
	// instruction's float type: exactly from F32 to F64, rounded to
	// nearest, ties to even, from F64 to F32.
	FToF
	// FToI produces args[0], of either float type, converted to the
	// instruction's integer type, truncated toward zero. A value the type
	// cannot represent, which Go leaves to the implementation, converts as
	// gc does on amd64 (D-032): with T32(x) the truncation of x if it lies
	// in [-2^31, 2^31) and -2^31 otherwise (NaN included), and T64(x) the
	// same for 64 bits, I8, I16, I32, U8, and U16 take the canonical form
	// of T32(x), I64 and U32 that of T64(x), and U64 is T64(x) if x < 2^63
	// and T64(x - 2^63) with bit 63 set otherwise.
	FToI
	// Str produces the address of Program.Strings[Index], a Ptr (D-033). No
	// args.
	Str
	// LoadB produces the U8 byte at offset args[1], an I64, of the string
	// whose address is args[0], a Ptr, as LdM.U8 does. Precondition: 0 <=
	// args[1] < the string's length (lowering checks it first, D-033).
	LoadB
	// Addr produces the address of Mem, a Ptr (D-034): of the function's
	// own instance of a local memory object, fresh for each call, or of a
	// global one. No args.
	Addr
	// LdM produces the value of its type held in memory at offset args[1],
	// an I64, of the memory object whose address is args[0], a Ptr; StM
	// writes args[2] there and produces no value. A value occupies
	// MemSize of its type bytes, little-endian: a Bool 0 or 1, an integer
	// its low width bits (LdM puts them in canonical form), an F32 its 32
	// bits, and an F64 or Ptr all 64. Preconditions (lowering guarantees
	// them, D-034): the bytes lie within the object, the offset is a
	// multiple of MemSize, and every byte LdM reads was written since the
	// object came into being, by StM, or for a global one by its zero
	// initialization.
	LdM
	StM
	// Alloc produces the Ptr of a new heap object of args[0] bytes, an I64,
	// all zero (D-035). The native heap is collected (D-046); with Const 1,
	// the object never holds a Ptr, and the collector does not look for
	// any in it (noscan, D-047). It is an effect: a call of the runtime. If
	// the heap is exhausted, the program ends with a fatal error.
	Alloc
	// PAdd produces args[0], a Ptr into an object, plus args[1] bytes, an
	// I64 (D-035). Precondition: the result lies in the same object or just
	// past its end, or args[1] is 0, when it is args[0], whatever that is
	// (nil included).
	PAdd
	// Move copies args[2] bytes, an I64, from the Ptr args[1] to the Ptr
	// args[0], as if through a temporary, so the ranges may overlap
	// (D-036). It produces no value; it is an effect, a call of the
	// runtime. Precondition: args[2] is 0, or each range lies in one object
	// and every byte of the source range has been written.
	Move
	// FuncAddr produces the address of the code of Callee, a function of
	// the program other than Init, as a Ptr (D-039). No args. Only CallInd
	// may use the address, directly or after it is stored in memory and
	// loaded again; LdM and StM never read through it.
	FuncAddr
	// CallInd calls the function whose code address args[0] is, a Ptr that
	// FuncAddr produced, with args[1:], one per entry of Sig.Params and of
	// the same type, and produces its result as Call does, typed by Sig's
	// Results (D-039). The callee's Params and Results must be Sig's.
	// Each call has a fresh set of slots.
	CallInd
	// GoNew produces the I64 handle of a new goroutine (D-048), which is
	// suspended: the first GoSwitch to it runs Callee, a function of the
	// program other than Init with no parameters and no results, on the
	// goroutine's own stack. Callee must never return; it ends with
	// GoExit. The handle is never 0, which is the main goroutine's (the one
	// that runs Init and main). No args. It is an effect, a call of the
	// runtime. If no goroutine stack is left (MaxGoroutines live), the
	// program ends with a fatal error.
	GoNew
	// GoSwitch suspends the running goroutine and resumes the goroutine
	// whose handle is args[0], an I64, where it last suspended (or starts
	// it). It produces no value; it returns when another GoSwitch, or a
	// GoExit, resumes this goroutine. Precondition: args[0] is the handle of
	// a live goroutine (0 or a GoNew's, not ended by GoExit).
	GoSwitch
	// GoExit ends the running goroutine and resumes the goroutine whose
	// handle is args[0], an I64, as GoSwitch does. It is a terminator.
	// Preconditions: the running goroutine is not the main one, and
	// args[0] is the handle of another live goroutine.
	GoExit
	// Sys is the system call Index, one of the Sys constants (D-049,
	// increment 11.6), made by the runtime. It is an effect, a call of the
	// runtime.
	Sys
)

// The system calls of Sys, with their args and value.
const (
	// SysExit ends the program with the exit status args[0], an I64,
	// modulo 256, as exit_group does; nothing else runs. No value.
	SysExit = iota
	// SysWrite writes args[2] (an I64) bytes at args[1] (a Ptr) to the
	// file descriptor args[0] (an I64) with one write system call. Its
	// value, an I64, is the bytes written, or the negated errno.
	SysWrite
	// SysArgc is the number of the program's arguments, its name
	// included: an I64. No args.
	SysArgc
	// SysArgPtr and SysArgLen are the address (a Ptr) and length (an
	// I64) of argument args[0], an I64 in [0, SysArgc).
	SysArgPtr
	SysArgLen
	// SysGC runs a garbage collection. No args, no value.
	SysGC
	// SysRead reads up to args[2] (an I64) bytes into the object at
	// args[1] (a Ptr) from the file descriptor args[0] (an I64) with one
	// read system call (increment 11.8). Its value, an I64, is the bytes
	// read, 0 at end of file, or the negated errno.
	SysRead
	// SysCall makes the Linux x86-64 system call numbered args[0] with the
	// arguments args[1] to args[6], all U64 (uintptr), an address passed as
	// its bits (D-051). Its value, an I64, is the system call's result: the negated
	// errno if in [-4095, -1].
	SysCall
	// SysEnvc is the number of the program's environment strings, as
	// execve passed them: an I64. No args.
	SysEnvc
	// SysEnvPtr and SysEnvLen are the address (a Ptr) and length (an I64)
	// of environment string args[0], an I64 in [0, SysEnvc).
	SysEnvPtr
	SysEnvLen
	// SysCPUFMA is 1 if the processor and the kernel provide AVX and FMA
	// as gc's internal/cpu decides (CPUID and XGETBV), else 0: an I64. No
	// args. gc's Exp computes otherwise with them (D-053).
	SysCPUFMA
	// SysRand is a random U64, as gc's runtime.rand: uniformly
	// distributed, from a generator the kernel's entropy seeds. No args
	// (D-053).
	SysRand
)

// SysNames are the system calls' names, as the printer writes them.
var SysNames = [...]string{SysExit: "exit", SysWrite: "write", SysArgc: "argc", SysArgPtr: "argptr", SysArgLen: "arglen", SysGC: "gc", SysRead: "read",
	SysCall: "syscall", SysEnvc: "envc", SysEnvPtr: "envptr", SysEnvLen: "envlen", SysCPUFMA: "cpufma", SysRand: "rand"}

var opNames = [...]string{
	invalidOp: "invalid", Const: "const", Load: "load", Store: "store",
	Add: "add", Sub: "sub", Mul: "mul", Neg: "neg", Println: "println", Ret: "ret",
	Param: "param", Call: "call", Jump: "jump", Branch: "branch",
	Eq: "eq", Ne: "ne", Lt: "lt", Le: "le", Gt: "gt", Ge: "ge", Not: "not",
	Extract: "extract", And: "and", Or: "or", Xor: "xor", AndNot: "andnot", Com: "com",
	Conv: "conv", Print: "print", Div: "div", Rem: "rem", Shl: "shl", Shr: "shr", Panic: "panic",
	GLoad: "gload", GStore: "gstore", Phi: "phi",
	FAdd: "fadd", FSub: "fsub", FMul: "fmul", FDiv: "fdiv", FNeg: "fneg",
	FEq: "feq", FNe: "fne", FLt: "flt", FLe: "fle", FGt: "fgt", FGe: "fge",
	IToF: "itof", FToF: "ftof", FToI: "ftoi", Str: "str", LoadB: "loadb",
	Addr: "addr", LdM: "ldm", StM: "stm", Alloc: "alloc", PAdd: "padd", Move: "move",
	FuncAddr: "funcaddr", CallInd: "callind",
	GoNew: "gonew", GoSwitch: "goswitch", GoExit: "goexit", Sys: "sys",
}

func (op Op) String() string {
	if int(op) < len(opNames) && opNames[op] != "" {
		return opNames[op]
	}
	return fmt.Sprintf("Op(%d)", uint8(op))
}

// IsTerminator reports whether op ends a block.
func (op Op) IsTerminator() bool {
	return op == Ret || op == Jump || op == Branch || op == Panic || op == GoExit
}

// The run-time errors of Panic (D-023: a closed set), with their messages.
const (
	PanicDivide = iota
	PanicShift
	PanicIndex
	PanicNil
	// The slice bounds errors (D-036), one per format of gc's
	// runtime/error.go: x is the bound out of range, y what it is compared
	// with.
	PanicSliceAlen  // s[:x] with x above the length y (of an array)
	PanicSliceAcap  // s[:x] with x above the capacity y
	PanicSliceB     // s[x:y] with x above y
	PanicSlice3Alen // s[::x] with x above the length y (of an array)
	PanicSlice3Acap // s[::x] with x above the capacity y
	PanicSlice3B    // s[:x:y] with x above y
	PanicSlice3C    // s[x:y:] with x above y
	PanicMakeLen    // make with a length out of range
	PanicMakeCap    // make with a capacity out of range
	PanicGrowLen    // append beyond the largest int
	// PanicMsg ends the program with a message made at run time, the
	// string args[0] (a Ptr) and args[1] (an I64): it writes "panic: ", the
	// message, and a newline, with no "runtime error: " of its own (D-041).
	PanicMsg
	// PanicExit ends the program with exit status 2 and writes nothing:
	// the panics have been written already (D-043). No args.
	PanicExit
	// PanicFatal ends the program with gc's fatal error whose message is
	// the string args[0] (a Ptr) and args[1] (an I64): it writes "fatal
	// error: ", the message, and a newline (D-048). It is never recovered
	// and runs no deferred calls.
	PanicFatal
)

// PanicMessages are the run-time errors' messages, as gc prints them after
// "panic: runtime error: ".
// The messages of PanicIndex and the slice bounds errors are completed by
// their operands, as BoundsMessage formats them.
var PanicMessages = [...]string{PanicDivide: "integer divide by zero", PanicShift: "negative shift amount", PanicIndex: "index out of range",
	PanicNil:       "invalid memory address or nil pointer dereference",
	PanicSliceAlen: "slice bounds out of range", PanicSliceAcap: "slice bounds out of range", PanicSliceB: "slice bounds out of range",
	PanicSlice3Alen: "slice bounds out of range", PanicSlice3Acap: "slice bounds out of range", PanicSlice3B: "slice bounds out of range",
	PanicSlice3C: "slice bounds out of range",
	PanicMakeLen: "makeslice: len out of range", PanicMakeCap: "makeslice: cap out of range", PanicGrowLen: "growslice: len out of range",
	PanicMsg: "", PanicExit: "", PanicFatal: ""}

// IsBounds reports whether the run-time error kind has operands: it is
// PanicIndex or a slice bounds error.
func IsBounds(kind int) bool {
	return kind == PanicIndex || kind >= PanicSliceAlen && kind <= PanicSlice3C
}

// boundsFormats are, for each run-time error with operands, the text before
// x, between x and y, and after y, and the text after a negative signed x,
// which replaces the rest (gc's runtime/error.go, go1.26.1, D-036).
var boundsFormats = map[int][4]string{
	PanicIndex:      {"[", "] with length ", "", "]"},
	PanicSliceAlen:  {"[:", "] with length ", "", "]"},
	PanicSliceAcap:  {"[:", "] with capacity ", "", "]"},
	PanicSliceB:     {"[", ":", "]", ":]"},
	PanicSlice3Alen: {"[::", "] with length ", "", "]"},
	PanicSlice3Acap: {"[::", "] with capacity ", "", "]"},
	PanicSlice3B:    {"[:", ":", "]", ":]"},
	PanicSlice3C:    {"[", ":", ":]", "::]"},
}

// BoundsFormat returns the texts of the message of PanicIndex or a slice
// bounds error kind around its operands, as BoundsMessage uses them:
// before x, between x and y, after y, and after a negative signed x.
func BoundsFormat(kind int) [4]string { return boundsFormats[kind] }

// BoundsMessage is the message of PanicIndex or a slice bounds error kind,
// with x, of a signed type if signed, and y, as gc writes it: a negative x
// has no y.
func BoundsMessage(kind int, x int64, signed bool, y int64) string {
	f := boundsFormats[kind]
	msg := PanicMessages[kind] + " " + f[0]
	switch {
	case signed && x < 0:
		return msg + fmt.Sprint(x) + f[3]
	case signed:
		msg += fmt.Sprint(x)
	default:
		msg += fmt.Sprint(uint64(x))
	}
	return msg + f[1] + fmt.Sprint(y) + f[2]
}

// IndexMessage is PanicIndex's message for index i, of a signed type if
// signed, and length n, as gc writes it (go1.26.1, D-033): a negative index
// has no length.
func IndexMessage(i int64, signed bool, n int64) string {
	return BoundsMessage(PanicIndex, i, signed, n)
}

// IsComparison reports whether op is one of Eq through Ge, the integer
// and Bool comparisons; the float comparisons are IsFloatComparison.
func (op Op) IsComparison() bool { return op >= Eq && op <= Ge }

// IsFloatComparison reports whether op is one of FEq through FGe.
func (op Op) IsFloatComparison() bool { return op >= FEq && op <= FGe }

// Pos is a source position, 1-based, in the file of the enclosing function.
type Pos struct {
	Line, Col int
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Program is a whole program. Its entry point is the function named "main".
// If Init is not nil, it runs before main: it is one of Funcs, named "init",
// takes no parameters, returns no results, and is never called (D-023).
// Strings are the bytes of its string constants, which Str addresses
// (D-033); none is empty, and none repeats. Mems are its global memory
// objects (D-034), which hold its package-level arrays and structs.
type Program struct {
	Funcs   []*Func
	Globals []*Global
	Init    *Func
	Strings []string
	Mems    []*Mem
}

// MaxMemSize bounds a memory object's size, so that every offset into one
// and every frame holding one fit comfortably in 32 bits (D-034).
const MaxMemSize = 1 << 30

// Mem is a memory object (D-034): Size bytes, at an address that is a
// multiple of Align, 1, 2, 4, or 8. A local one, of Func.Mems, exists once
// for each call of its function, from the call to its return, and holds
// nothing defined until stored to; a global one, of Program.Mems, exists for
// the whole run and starts as zero bytes.
type Mem struct {
	ID          int    // index in Func.Mems or Program.Mems
	Name        string // the source name, for printing and symbols
	Size, Align int64
	Global      bool
	File        string // for a global one, the source file of Pos
	Pos         Pos
}

// Intern returns the index of s in p.Strings, adding it if it is new. s
// must not be empty.
func (p *Program) Intern(s string) int {
	for i, t := range p.Strings {
		if t == s {
			return i
		}
	}
	p.Strings = append(p.Strings, s)
	return len(p.Strings) - 1
}

// Global is a package-level variable. It holds the zero value of its type
// until it is stored to.
type Global struct {
	ID   int    // index in Program.Globals
	Name string // the source name, unique in the program
	Type Type
	File string // source file of Pos
	Pos  Pos
}

// Func is a function. Main takes no parameters and returns no results.
type Func struct {
	Name string
	File string // source file of every position in the function
	Pos  Pos
	// Params and Results are the types of the parameters and results.
	Params, Results []Type
	// Slots are the function's local variables. A slot has no defined
	// contents until it is stored to.
	Slots []*Slot
	// Mems are its local memory objects (D-034).
	Mems []*Mem
	// Blocks are the function's basic blocks; Blocks[0] is the entry.
	Blocks []*Block
	// SSA reports whether the function is in SSA form (D-025): it has no
	// slots, values may be used in any block their definition dominates,
	// and phis merge them where control flow joins.
	SSA bool

	nextValue int
}

// Slot is a local variable's storage.
type Slot struct {
	ID   int    // index in Func.Slots
	Name string // the source name, for printing only; names may repeat
	Type Type
	Pos  Pos
}

// Block is a basic block: instructions executed in order, the last of which
// is the block's only terminator.
type Block struct {
	ID     int // index in Func.Blocks
	Instrs []*Instr
}

// Instr is an instruction. If its Type is not NoType it produces a value,
// numbered ID, and other instructions may use it as an argument.
type Instr struct {
	Op      Op
	Type    Type
	ID      int // value number, unique in the function; -1 if no value
	Args    []*Instr
	Slot    *Slot    // for Load and Store
	Const   int64    // for Const; for Alloc, 1 if noscan
	Index   int      // for Param, Extract, and Panic
	Callee  *Func    // for Call and FuncAddr
	Sig     *Sig     // for CallInd
	Global  *Global  // for GLoad and GStore
	Mem     *Mem     // for Addr
	Targets []*Block // for Jump and Branch
	Edges   []Edge   // for Phi: the incoming edge of each arg
	Pos     Pos
}

// Sig is the signature of an indirect call: the types of the callee's
// parameters and results (D-039).
type Sig struct {
	Params, Results []Type
}

// CallResults returns the result types of a Call or CallInd.
func (in *Instr) CallResults() []Type {
	if in.Op == CallInd {
		return in.Sig.Results
	}
	return in.Callee.Results
}

// IsCall reports whether in calls a function of the program: a Call or a
// CallInd.
func (in *Instr) IsCall() bool { return in.Op == Call || in.Op == CallInd }

// CallArgs returns the arguments a Call or CallInd passes to its callee:
// a CallInd's args after the code address.
func (in *Instr) CallArgs() []*Instr {
	if in.Op == CallInd {
		return in.Args[1:]
	}
	return in.Args
}

// Edge is a control-flow edge: the one from block From to target Index of
// its terminator. A branch whose two targets are the same block has two
// edges to it, told apart by Index.
type Edge struct {
	From  *Block
	Index int
}

// NewFunc returns an empty function with an entry block.
func (p *Program) NewFunc(name, file string, pos Pos) *Func {
	f := &Func{Name: name, File: file, Pos: pos}
	f.Blocks = []*Block{{ID: 0}}
	p.Funcs = append(p.Funcs, f)
	return f
}

// NewGlobal adds a global variable to p.
func (p *Program) NewGlobal(name string, t Type, file string, pos Pos) *Global {
	g := &Global{ID: len(p.Globals), Name: name, Type: t, File: file, Pos: pos}
	p.Globals = append(p.Globals, g)
	return g
}

// NewMem adds a global memory object to p.
func (p *Program) NewMem(name string, size, align int64, file string, pos Pos) *Mem {
	m := &Mem{ID: len(p.Mems), Name: name, Size: size, Align: align, Global: true, File: file, Pos: pos}
	p.Mems = append(p.Mems, m)
	return m
}

// NewMem adds a local memory object to f.
func (f *Func) NewMem(name string, size, align int64, pos Pos) *Mem {
	m := &Mem{ID: len(f.Mems), Name: name, Size: size, Align: align, Pos: pos}
	f.Mems = append(f.Mems, m)
	return m
}

// NewBlock adds an empty block to f.
func (f *Func) NewBlock() *Block {
	b := &Block{ID: len(f.Blocks)}
	f.Blocks = append(f.Blocks, b)
	return b
}

// NewSlot adds a local variable slot to f.
func (f *Func) NewSlot(name string, t Type, pos Pos) *Slot {
	s := &Slot{ID: len(f.Slots), Name: name, Type: t, Pos: pos}
	f.Slots = append(f.Slots, s)
	return s
}

// Entry returns f's entry block.
func (f *Func) Entry() *Block { return f.Blocks[0] }

// The methods below append an instruction to block b of function f and
// return it. Value numbers are assigned in order of creation, so building
// the same program twice gives identical numbering.

func (f *Func) add(b *Block, in *Instr) *Instr {
	in.ID = -1
	if in.Type != NoType {
		in.ID = f.nextValue
		f.nextValue++
	}
	b.Instrs = append(b.Instrs, in)
	return in
}

// Const appends an I64 constant.
func (f *Func) Const(b *Block, v int64, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Const, Type: I64, Const: v, Pos: pos})
}

// TypedConst appends a constant of type t; v must be in canonical form.
func (f *Func) TypedConst(b *Block, t Type, v int64, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Const, Type: t, Const: v, Pos: pos})
}

// FloatConst appends the float constant v of type t, F64 or F32; for F32,
// v must be exactly representable.
func (f *Func) FloatConst(b *Block, t Type, v float64, pos Pos) *Instr {
	bits := int64(math.Float64bits(v))
	if t == F32 {
		bits = int64(math.Float32bits(float32(v)))
	}
	return f.add(b, &Instr{Op: Const, Type: t, Const: bits, Pos: pos})
}

// Float appends the float operation op (FAdd through FNeg, FEq through
// FGe) of args, of the type of args[0].
func (f *Func) Float(b *Block, op Op, pos Pos, args ...*Instr) *Instr {
	t := args[0].Type
	if op.IsFloatComparison() {
		t = Bool
	}
	return f.add(b, &Instr{Op: op, Type: t, Args: args, Pos: pos})
}

// Convert appends IToF, FToF, or FToI of x to t.
func (f *Func) Convert(b *Block, op Op, t Type, x *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: op, Type: t, Args: []*Instr{x}, Pos: pos})
}

// Conv appends the conversion of x to t.
func (f *Func) Conv(b *Block, t Type, x *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Conv, Type: t, Args: []*Instr{x}, Pos: pos})
}

// Print appends a print of args.
func (f *Func) Print(b *Block, args []*Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Print, Args: args, Pos: pos})
}

// BoolConst appends a Bool constant.
func (f *Func) BoolConst(b *Block, v bool, pos Pos) *Instr {
	in := &Instr{Op: Const, Type: Bool, Pos: pos}
	if v {
		in.Const = 1
	}
	return f.add(b, in)
}

// Compare appends the comparison op of x and y.
func (f *Func) Compare(b *Block, op Op, x, y *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: op, Type: Bool, Args: []*Instr{x, y}, Pos: pos})
}

// Not appends the negation of x.
func (f *Func) Not(b *Block, x *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Not, Type: Bool, Args: []*Instr{x}, Pos: pos})
}

// Jump appends a jump to target.
func (f *Func) Jump(b, target *Block, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Jump, Targets: []*Block{target}, Pos: pos})
}

// Branch appends a branch on cond to t or e.
func (f *Func) Branch(b *Block, cond *Instr, t, e *Block, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Branch, Args: []*Instr{cond}, Targets: []*Block{t, e}, Pos: pos})
}

// Load appends a load of s.
func (f *Func) Load(b *Block, s *Slot, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Load, Type: s.Type, Slot: s, Pos: pos})
}

// GLoad appends a load of g.
func (f *Func) GLoad(b *Block, g *Global, pos Pos) *Instr {
	return f.add(b, &Instr{Op: GLoad, Type: g.Type, Global: g, Pos: pos})
}

// GStore appends a store of v to g.
func (f *Func) GStore(b *Block, g *Global, v *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: GStore, Args: []*Instr{v}, Global: g, Pos: pos})
}

// Store appends a store of v to s.
func (f *Func) Store(b *Block, s *Slot, v *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Store, Args: []*Instr{v}, Slot: s, Pos: pos})
}

// Binary appends Add, Sub, Mul, And, Or, Xor, or AndNot of x and y, of
// x's type.
func (f *Func) Binary(b *Block, op Op, x, y *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: op, Type: x.Type, Args: []*Instr{x, y}, Pos: pos})
}

// Neg appends the negation of x.
func (f *Func) Neg(b *Block, x *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Neg, Type: x.Type, Args: []*Instr{x}, Pos: pos})
}

// Shift appends Shl or Shr of x by count.
func (f *Func) Shift(b *Block, op Op, x, count *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: op, Type: x.Type, Args: []*Instr{x, count}, Pos: pos})
}

// Panic appends a panic with the run-time error kind and its args.
func (f *Func) Panic(b *Block, kind int, pos Pos, args ...*Instr) *Instr {
	return f.add(b, &Instr{Op: Panic, Index: kind, Args: args, Pos: pos})
}

// Str appends the address of string i of the program.
func (f *Func) Str(b *Block, i int, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Str, Type: Ptr, Index: i, Pos: pos})
}

// LoadB appends the load of byte i of the string at ptr.
func (f *Func) LoadB(b *Block, ptr, i *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: LoadB, Type: U8, Args: []*Instr{ptr, i}, Pos: pos})
}

// Addr appends the address of m.
func (f *Func) Addr(b *Block, m *Mem, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Addr, Type: Ptr, Mem: m, Pos: pos})
}

// LdM appends the load of a t at offset off of the memory object at p.
func (f *Func) LdM(b *Block, t Type, p, off *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: LdM, Type: t, Args: []*Instr{p, off}, Pos: pos})
}

// Alloc appends the allocation of a heap object of n bytes.
func (f *Func) Alloc(b *Block, n *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Alloc, Type: Ptr, Args: []*Instr{n}, Pos: pos})
}

// AllocNoScan appends the allocation of a heap object of n bytes that will
// never hold a Ptr (D-047).
func (f *Func) AllocNoScan(b *Block, n *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Alloc, Type: Ptr, Args: []*Instr{n}, Const: 1, Pos: pos})
}

// PAdd appends p plus i bytes.
func (f *Func) PAdd(b *Block, p, i *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: PAdd, Type: Ptr, Args: []*Instr{p, i}, Pos: pos})
}

// Move appends a move of n bytes from src to dst.
func (f *Func) Move(b *Block, dst, src, n *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Move, Args: []*Instr{dst, src, n}, Pos: pos})
}

// StM appends the store of v at offset off of the memory object at p.
func (f *Func) StM(b *Block, p, off, v *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: StM, Args: []*Instr{p, off, v}, Pos: pos})
}

// Com appends the complement of x.
func (f *Func) Com(b *Block, x *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Com, Type: x.Type, Args: []*Instr{x}, Pos: pos})
}

// Println appends a println of args.
func (f *Func) Println(b *Block, args []*Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Println, Args: args, Pos: pos})
}

// Ret appends a return of results.
func (f *Func) Ret(b *Block, pos Pos, results ...*Instr) *Instr {
	return f.add(b, &Instr{Op: Ret, Args: results, Pos: pos})
}

// Param appends parameter i of f.
func (f *Func) Param(b *Block, i int, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Param, Type: f.Params[i], Index: i, Pos: pos})
}

// Call appends a call of callee with args.
func (f *Func) Call(b *Block, callee *Func, args []*Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Call, Type: callType(callee.Results), Args: args, Callee: callee, Pos: pos})
}

// FuncAddr appends the address of callee's code (D-039).
func (f *Func) FuncAddr(b *Block, callee *Func, pos Pos) *Instr {
	return f.add(b, &Instr{Op: FuncAddr, Type: Ptr, Callee: callee, Pos: pos})
}

// GoNew appends the creation of a goroutine that will run f (D-048).
func (f *Func) GoNew(b *Block, callee *Func, pos Pos) *Instr {
	return f.add(b, &Instr{Op: GoNew, Type: I64, Callee: callee, Pos: pos})
}

// GoSwitch appends a switch to the goroutine whose handle is h.
func (f *Func) GoSwitch(b *Block, h *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: GoSwitch, Args: []*Instr{h}, Pos: pos})
}

// GoExit appends the end of the running goroutine and a switch to h.
func (f *Func) GoExit(b *Block, h *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: GoExit, Args: []*Instr{h}, Pos: pos})
}

// Sys appends the system call kind, of type t (NoType for none), with
// args.
func (f *Func) Sys(b *Block, kind int, t Type, pos Pos, args ...*Instr) *Instr {
	return f.add(b, &Instr{Op: Sys, Type: t, Index: kind, Args: args, Pos: pos})
}

// CallInd appends a call, of signature sig, of the code at the address
// code, with args (D-039).
func (f *Func) CallInd(b *Block, sig *Sig, code *Instr, args []*Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: CallInd, Type: callType(sig.Results), Args: append([]*Instr{code}, args...), Sig: sig, Pos: pos})
}

// callType is the type of a call of a function with results rs.
func callType(rs []Type) Type {
	switch len(rs) {
	case 0:
		return NoType
	case 1:
		return rs[0]
	}
	return Tuple
}

// Phi inserts a phi of type t with no incoming values into b, after b's
// existing phis. AddIncoming gives it its values.
func (f *Func) Phi(b *Block, t Type, pos Pos) *Instr {
	n := 0
	for n < len(b.Instrs) && b.Instrs[n].Op == Phi {
		n++
	}
	in := f.add(b, &Instr{Op: Phi, Type: t, Pos: pos})
	copy(b.Instrs[n+1:], b.Instrs[n:])
	b.Instrs[n] = in
	return in
}

// AddIncoming appends v as phi's value along edge e.
func (phi *Instr) AddIncoming(e Edge, v *Instr) {
	phi.Edges = append(phi.Edges, e)
	phi.Args = append(phi.Args, v)
}

// Extract appends result i of call, a Tuple call.
func (f *Func) Extract(b *Block, call *Instr, i int, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Extract, Type: call.CallResults()[i], Args: []*Instr{call}, Index: i, Pos: pos})
}
