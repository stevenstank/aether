// Package ir defines Aether's intermediate representation (D-004, D-021).
//
// The IR is Aether's own: it imports no go/* package, and lowering is the
// only stage that translates from the type-checked Go syntax into it. Its
// meaning is specified here and in docs/ARCHITECTURE.md §4, precisely enough
// that a backend or interpreter can consume it without consulting Go source.
//
// The instruction set covers subset S1 (docs/SEMANTICS.md), the calls of
// D-023 increment 4.2, and the control flow and bool of increment 4.1. A program is a set of functions; a function owns its local variable slots
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
)

func (t Type) String() string {
	switch t {
	case NoType:
		return "none"
	case I64:
		return "i64"
	case Bool:
		return "bool"
	}
	return fmt.Sprintf("Type(%d)", uint8(t))
}

// Op is an instruction's operation. The meaning of each operation is given
// with its constant; "args" are the instruction's value operands, in order.
type Op uint8

const (
	invalidOp Op = iota
	// Const produces the constant Const of its type: any value for I64, 0
	// or 1 for Bool. No args.
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
	// Println writes its args to standard error, I64 in signed decimal
	// and Bool as true or false, separated by single spaces and followed
	// by a newline, in that order.
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
	// args[0] with args[1], which have the same type: signed for I64; only
	// Eq and Ne for Bool.
	Eq
	Ne
	Lt
	Le
	Gt
	Ge
	// Not produces the negation of args[0], a Bool.
	Not
	// Param produces the value of parameter Index of the function. No
	// args. It appears only in the entry block.
	Param
	// Call calls Callee, a function of the program, with args, one per
	// entry of Callee.Params and of the same type, and produces its result:
	// the type is Callee's single result type, or NoType if it has none.
	// Each call has a fresh set of slots.
	Call
)

var opNames = [...]string{
	invalidOp: "invalid", Const: "const", Load: "load", Store: "store",
	Add: "add", Sub: "sub", Mul: "mul", Neg: "neg", Println: "println", Ret: "ret",
	Param: "param", Call: "call", Jump: "jump", Branch: "branch",
	Eq: "eq", Ne: "ne", Lt: "lt", Le: "le", Gt: "gt", Ge: "ge", Not: "not",
}

func (op Op) String() string {
	if int(op) < len(opNames) && opNames[op] != "" {
		return opNames[op]
	}
	return fmt.Sprintf("Op(%d)", uint8(op))
}

// IsTerminator reports whether op ends a block.
func (op Op) IsTerminator() bool { return op == Ret || op == Jump || op == Branch }

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
	// Params and Results are the types of the parameters and results. A
	// function has at most one result (D-023, increment 4.2).
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
	Index   int      // for Param
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
	t := NoType
	if len(callee.Results) == 1 {
		t = callee.Results[0]
	}
	return f.add(b, &Instr{Op: Call, Type: t, Args: args, Callee: callee, Pos: pos})
}
