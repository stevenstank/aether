package ir

import "fmt"

// VerifyError is one violated invariant. It names the function, and the
// block and instruction where applicable, so a failure points at the
// malformed IR rather than at the user's source.
type VerifyError struct {
	Func  string
	Block int // -1 if not about a block
	Index int // instruction index in the block; -1 if not about one
	Instr string
	Msg   string
}

func (e *VerifyError) Error() string {
	s := "func " + e.Func
	if e.Block >= 0 {
		s += fmt.Sprintf(": b%d", e.Block)
	}
	if e.Index >= 0 {
		s += fmt.Sprintf(": instr %d (%s)", e.Index, e.Instr)
	}
	return s + ": " + e.Msg
}

// Verify checks the invariants of docs/ARCHITECTURE.md §4 that apply to
// this IR and returns every violation found, in a deterministic order. An
// empty result means p is well formed. Lowering must only ever produce
// well-formed IR, so any error is an internal compiler error.
func Verify(p *Program) []error {
	var errs []error
	names := map[string]bool{}
	mains := 0
	for _, f := range p.Funcs {
		if f == nil {
			errs = append(errs, &VerifyError{Func: "?", Block: -1, Index: -1, Msg: "nil function"})
			continue
		}
		if names[f.Name] {
			errs = append(errs, &VerifyError{Func: f.Name, Block: -1, Index: -1, Msg: "duplicate function name"})
		}
		names[f.Name] = true
		if f.Name == "main" {
			mains++
		}
		errs = append(errs, verifyFunc(f)...)
	}
	if mains == 0 {
		errs = append(errs, &VerifyError{Func: "main", Block: -1, Index: -1, Msg: "missing entry point: no function named main"})
	}
	return errs
}

type verifier struct {
	f    *Func
	errs []error
}

func (v *verifier) errorf(block, index int, in *Instr, format string, args ...any) {
	e := &VerifyError{Func: v.f.Name, Block: block, Index: index, Msg: fmt.Sprintf(format, args...)}
	if in != nil {
		e.Instr = in.String()
	}
	v.errs = append(v.errs, e)
}

func verifyFunc(f *Func) []error {
	v := &verifier{f: f}
	if f.File == "" || !validPos(f.Pos) {
		v.errorf(-1, -1, nil, "missing source position")
	}
	for i, s := range f.Slots {
		switch {
		case s == nil:
			v.errorf(-1, -1, nil, "slot %d is nil", i)
		case s.ID != i:
			v.errorf(-1, -1, nil, "slot %d has ID %d", i, s.ID)
		case s.Type != I64:
			v.errorf(-1, -1, nil, "slot s%d has invalid type %s", i, s.Type)
		case !validPos(s.Pos):
			v.errorf(-1, -1, nil, "slot s%d has no source position", i)
		}
	}
	if len(f.Blocks) == 0 {
		v.errorf(-1, -1, nil, "no blocks")
		return v.errs
	}
	// The IR has no branch instructions, so no block can transfer control
	// to another: every block except the entry is unreachable.
	for i, b := range f.Blocks {
		if b == nil {
			v.errorf(i, -1, nil, "nil block")
			continue
		}
		if b.ID != i {
			v.errorf(i, -1, nil, "block has ID %d", b.ID)
		}
		if i > 0 {
			v.errorf(i, -1, nil, "unreachable block: no instruction branches to it")
		}
		v.block(b, i)
	}
	return v.errs
}

// block checks one block. Because the IR has no branches, the instructions
// before a use are exactly those that execute before it, so "defined
// earlier in the block" and "stored earlier in the block" are exact
// dominance and definite-assignment checks.
func (v *verifier) block(b *Block, bi int) {
	if len(b.Instrs) == 0 {
		v.errorf(bi, -1, nil, "empty block: missing terminator")
		return
	}
	defined := map[*Instr]bool{}
	stored := map[*Slot]bool{}
	ids := map[int]bool{}
	for i, in := range b.Instrs {
		if in == nil {
			v.errorf(bi, i, nil, "nil instruction")
			continue
		}
		last := i == len(b.Instrs)-1
		switch {
		case in.Op.IsTerminator() && !last:
			v.errorf(bi, i, in, "terminator before the end of the block")
		case !in.Op.IsTerminator() && last:
			v.errorf(bi, i, in, "missing terminator: block does not end in ret")
		}
		if !validPos(in.Pos) {
			v.errorf(bi, i, in, "missing source position")
		}
		v.signature(bi, i, in)
		for _, a := range in.Args {
			switch {
			case a == nil:
				v.errorf(bi, i, in, "nil operand")
			case !defined[a]:
				v.errorf(bi, i, in, "operand v%d is not defined earlier in this block", a.ID)
			case a.Type != I64:
				v.errorf(bi, i, in, "operand v%d has type %s, want i64", a.ID, a.Type)
			}
		}
		if in.Op == Load || in.Op == Store {
			v.slotRef(bi, i, in, stored)
		}
		if in.Type != NoType {
			if ids[in.ID] {
				v.errorf(bi, i, in, "value number v%d defined twice", in.ID)
			}
			ids[in.ID] = true
			defined[in] = true
		}
	}
}

// signature checks an instruction's result type, value number, and operand
// count against its operation.
func (v *verifier) signature(bi, i int, in *Instr) {
	var want Type
	var nargs int // -1: any number
	switch in.Op {
	case Const, Load:
		want, nargs = I64, 0
	case Store:
		want, nargs = NoType, 1
	case Add, Sub, Mul:
		want, nargs = I64, 2
	case Neg:
		want, nargs = I64, 1
	case Println:
		want, nargs = NoType, -1
	case Ret:
		want, nargs = NoType, 0
	default:
		v.errorf(bi, i, in, "invalid operation %s", in.Op)
		return
	}
	if in.Type != want {
		v.errorf(bi, i, in, "%s has result type %s, want %s", in.Op, in.Type, want)
	}
	if (in.Type == NoType) != (in.ID == -1) {
		v.errorf(bi, i, in, "value number %d inconsistent with result type %s", in.ID, in.Type)
	}
	if nargs >= 0 && len(in.Args) != nargs {
		v.errorf(bi, i, in, "%s has %d operands, want %d", in.Op, len(in.Args), nargs)
	}
	if in.Op != Const && in.Const != 0 {
		v.errorf(bi, i, in, "%s has a constant operand", in.Op)
	}
	if in.Op != Load && in.Op != Store && in.Slot != nil {
		v.errorf(bi, i, in, "%s has a slot operand", in.Op)
	}
}

// slotRef checks a Load or Store's slot: it must belong to the function,
// and a load must follow a store to the same slot.
func (v *verifier) slotRef(bi, i int, in *Instr, stored map[*Slot]bool) {
	s := in.Slot
	if s == nil || s.ID < 0 || s.ID >= len(v.f.Slots) || v.f.Slots[s.ID] != s {
		v.errorf(bi, i, in, "slot operand is not a slot of this function")
		return
	}
	if in.Op == Store {
		stored[s] = true
		return
	}
	if !stored[s] {
		v.errorf(bi, i, in, "load of s%d (%s) before any store to it", s.ID, s.Name)
	}
}

func validPos(p Pos) bool { return p.Line > 0 && p.Col > 0 }
