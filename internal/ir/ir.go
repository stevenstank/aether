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
// bit operations, and print of increment 4.4, and the division, shifts, and
// panics of increment 4.5. A program is a set of functions; a function owns its local variable slots
// and a list of basic blocks; a block is a sequence of instructions ending in
// one terminator. An instruction that produces a value is that value:
// operands refer to such instructions directly.
package ir

import "fmt"

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
)

var typeNames = [...]string{
	NoType: "none", I64: "i64", Bool: "bool", Tuple: "tuple",
	I8: "i8", I16: "i16", I32: "i32", U8: "u8", U16: "u16", U32: "u32", U64: "u64",
}

func (t Type) String() string {
	if int(t) < len(typeNames) && typeNames[t] != "" {
		return typeNames[t]
	}
	return fmt.Sprintf("Type(%d)", uint8(t))
}

// IsInteger reports whether t is one of the integer types.
func (t Type) IsInteger() bool { return t == I64 || t >= I8 && t <= U64 }

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
// zero-extended if not. For a Bool or I64 it returns v unchanged.
func (t Type) Canonical(v int64) int64 {
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
	// Panic ends the program with the run-time error Index (PanicDivide or
	// PanicShift): the message and exit status 2. It is a terminator. No
	// args.
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
)

var opNames = [...]string{
	invalidOp: "invalid", Const: "const", Load: "load", Store: "store",
	Add: "add", Sub: "sub", Mul: "mul", Neg: "neg", Println: "println", Ret: "ret",
	Param: "param", Call: "call", Jump: "jump", Branch: "branch",
	Eq: "eq", Ne: "ne", Lt: "lt", Le: "le", Gt: "gt", Ge: "ge", Not: "not",
	Extract: "extract", And: "and", Or: "or", Xor: "xor", AndNot: "andnot", Com: "com",
	Conv: "conv", Print: "print", Div: "div", Rem: "rem", Shl: "shl", Shr: "shr", Panic: "panic",
}

func (op Op) String() string {
	if int(op) < len(opNames) && opNames[op] != "" {
		return opNames[op]
	}
	return fmt.Sprintf("Op(%d)", uint8(op))
}

// IsTerminator reports whether op ends a block.
func (op Op) IsTerminator() bool { return op == Ret || op == Jump || op == Branch || op == Panic }

// The run-time errors of Panic (D-023: a closed set), with their messages.
const (
	PanicDivide = iota
	PanicShift
)

// PanicMessages are the run-time errors' messages, as gc prints them after
// "panic: runtime error: ".
var PanicMessages = [...]string{PanicDivide: "integer divide by zero", PanicShift: "negative shift amount"}

// IsComparison reports whether op is one of Eq through Ge.
func (op Op) IsComparison() bool { return op >= Eq && op <= Ge }

// Pos is a source position, 1-based, in the file of the enclosing function.
type Pos struct {
	Line, Col int
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Program is a whole program. Its entry point is the function named "main".
type Program struct {
	Funcs []*Func
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
	// Blocks are the function's basic blocks; Blocks[0] is the entry.
	Blocks []*Block

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
	Const   int64    // for Const
	Index   int      // for Param, Extract, and Panic
	Callee  *Func    // for Call
	Targets []*Block // for Jump and Branch
	Pos     Pos
}

// NewFunc returns an empty function with an entry block.
func (p *Program) NewFunc(name, file string, pos Pos) *Func {
	f := &Func{Name: name, File: file, Pos: pos}
	f.Blocks = []*Block{{ID: 0}}
	p.Funcs = append(p.Funcs, f)
	return f
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

// Panic appends a panic with the run-time error kind.
func (f *Func) Panic(b *Block, kind int, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Panic, Index: kind, Pos: pos})
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
	return f.add(b, &Instr{Op: Call, Type: callType(callee), Args: args, Callee: callee, Pos: pos})
}

// callType is the type of a call of callee.
func callType(callee *Func) Type {
	switch len(callee.Results) {
	case 0:
		return NoType
	case 1:
		return callee.Results[0]
	}
	return Tuple
}

// Extract appends result i of call, a Tuple call.
func (f *Func) Extract(b *Block, call *Instr, i int, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Extract, Type: call.Callee.Results[i], Args: []*Instr{call}, Index: i, Pos: pos})
}
