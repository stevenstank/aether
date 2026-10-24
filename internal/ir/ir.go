// Package ir defines Aether's intermediate representation (D-004, D-021).
//
// The IR is Aether's own: it imports no go/* package, and lowering is the
// only stage that translates from the type-checked Go syntax into it. Its
// meaning is specified here and in docs/ARCHITECTURE.md §4, precisely enough
// that a backend or interpreter can consume it without consulting Go source.
//
// The instruction set covers exactly subset S1 (docs/SEMANTICS.md). A
// program is a set of functions; a function owns its local variable slots
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
)

func (t Type) String() string {
	switch t {
	case NoType:
		return "none"
	case I64:
		return "i64"
	}
	return fmt.Sprintf("Type(%d)", uint8(t))
}

// Op is an instruction's operation. The meaning of each operation is given
// with its constant; "args" are the instruction's value operands, in order.
type Op uint8

const (
	invalidOp Op = iota
	// Const produces the I64 constant Const. No args.
	Const
	// Load produces the current contents of Slot. No args. The slot must
	// have been stored to earlier on every path to the load.
	Load
	// Store writes args[0] to Slot. It produces no value.
	Store
	// Add, Sub, and Mul produce args[0] op args[1], wrapping modulo 2^64
	// in two's complement: the low 64 bits of the exact result. They never
	// trap.
	Add
	Sub
	Mul
	// Neg produces 0 - args[0], wrapping; Neg of the minimum I64 is the
	// minimum I64.
	Neg
	// Println writes its args to standard error in signed decimal,
	// separated by single spaces and followed by a newline, in that order.
	// The args are already-evaluated values, so all of them are computed
	// before any output. Write errors are ignored (D-009). It produces no
	// value.
	Println
	// Ret ends the function. It is a terminator and produces no value.
	Ret
)

var opNames = [...]string{
	invalidOp: "invalid", Const: "const", Load: "load", Store: "store",
	Add: "add", Sub: "sub", Mul: "mul", Neg: "neg", Println: "println", Ret: "ret",
}

func (op Op) String() string {
	if int(op) < len(opNames) && opNames[op] != "" {
		return opNames[op]
	}
	return fmt.Sprintf("Op(%d)", uint8(op))
}

// IsTerminator reports whether op ends a block.
func (op Op) IsTerminator() bool { return op == Ret }

// Pos is a source position, 1-based, in the file of the enclosing function.
type Pos struct {
	Line, Col int
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Program is a whole program. Its entry point is the function named "main".
type Program struct {
	Funcs []*Func
}

// Func is a function. In S1, functions take no parameters and return no
// results.
type Func struct {
	Name string
	File string // source file of every position in the function
	Pos  Pos
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
	Op    Op
	Type  Type
	ID    int // value number, unique in the function; -1 if no value
	Args  []*Instr
	Slot  *Slot // for Load and Store
	Const int64 // for Const
	Pos   Pos
}

// NewFunc returns an empty function with an entry block.
func (p *Program) NewFunc(name, file string, pos Pos) *Func {
	f := &Func{Name: name, File: file, Pos: pos}
	f.Blocks = []*Block{{ID: 0}}
	p.Funcs = append(p.Funcs, f)
	return f
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

// Const appends a constant.
func (f *Func) Const(b *Block, v int64, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Const, Type: I64, Const: v, Pos: pos})
}

// Load appends a load of s.
func (f *Func) Load(b *Block, s *Slot, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Load, Type: s.Type, Slot: s, Pos: pos})
}

// Store appends a store of v to s.
func (f *Func) Store(b *Block, s *Slot, v *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Store, Args: []*Instr{v}, Slot: s, Pos: pos})
}

// Binary appends Add, Sub, or Mul of x and y.
func (f *Func) Binary(b *Block, op Op, x, y *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: op, Type: I64, Args: []*Instr{x, y}, Pos: pos})
}

// Neg appends the negation of x.
func (f *Func) Neg(b *Block, x *Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Neg, Type: I64, Args: []*Instr{x}, Pos: pos})
}

// Println appends a println of args.
func (f *Func) Println(b *Block, args []*Instr, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Println, Args: args, Pos: pos})
}

// Ret appends a return.
func (f *Func) Ret(b *Block, pos Pos) *Instr {
	return f.add(b, &Instr{Op: Ret, Pos: pos})
}
