package lower

// The unwinding pass (D-043): in a program with a defer statement, once
// lowering ends, every run-time error raises a panic and goes to its
// function's exit, and every call of a function that may raise is
// followed by a check of .unwinding that goes there too.

import (
	"github.com/stevenstank/aether/internal/ir"
)

// unwind rewrites every function of the program but the panic state's
// own, which never raise.
func (l *lowerer) unwind() {
	ps := l.ps
	own := map[*ir.Func]bool{ps.gopanic: true, ps.raise: true, ps.printpanics: true, ps.preprint: true,
		ps.printv: true, ps.chain: true, ps.indented: true, ps.hex: true}
	raising := l.raising(own)
	for _, f := range append([]*ir.Func(nil), l.prog.Funcs...) {
		if !own[f] {
			l.unwindFunc(f, raising)
		}
	}
	l.fn = nil
}

// raising returns the functions that may return with .unwinding set:
// those with a run-time error or a panic, an indirect call, or a call of
// one of them.
func (l *lowerer) raising(own map[*ir.Func]bool) map[*ir.Func]bool {
	raising := map[*ir.Func]bool{}
	for changed := true; changed; {
		changed = false
		for _, f := range l.prog.Funcs {
			if raising[f] || own[f] {
				continue
			}
		scan:
			for _, b := range f.Blocks {
				for _, in := range b.Instrs {
					if in.Op == ir.Panic && !l.ps.final[in] || in.Op == ir.CallInd && !l.ps.noCheck[in] || in.Op == ir.Call && raising[in.Callee] {
						raising[f] = true
						changed = true
						break scan
					}
				}
			}
		}
	}
	return raising
}

// exit returns the block a raise in f goes to: f's deferred calls, or a
// return, or for main and Init the end of the program.
func (l *lowerer) exit(f *ir.Func) *ir.Block {
	if b := l.ps.exits[f]; b != nil {
		return b
	}
	b := f.NewBlock()
	l.ps.exits[f] = b
	l.b = b
	pos := f.Pos
	if f.Name == "main" || f == l.prog.Init {
		l.endProgram(pos)
		return b
	}
	var zeros []*ir.Instr
	for _, t := range f.Results {
		zeros = append(zeros, f.TypedConst(b, t, 0, pos))
	}
	f.Ret(b, pos, zeros...)
	return b
}

// unwindFunc rewrites f's run-time errors and panics and adds the checks
// after its calls.
func (l *lowerer) unwindFunc(f *ir.Func, raising map[*ir.Func]bool) {
	l.fn = f
	ps := l.ps
	for bi := 0; bi < len(f.Blocks); bi++ {
		b := f.Blocks[bi]
		for i := 0; i < len(b.Instrs); i++ {
			in := b.Instrs[i]
			switch {
			case in.Op == ir.Panic && !ps.final[in]:
				b.Instrs = b.Instrs[:i]
				l.b = b
				if in.Index != ir.PanicExit {
					l.raiseRuntime(f, b, in)
				}
				f.Jump(b, l.exit(f), in.Pos)
			case in.IsCall() && !ps.noCheck[in] && (in.Op == ir.CallInd || raising[in.Callee]):
				l.splitAfter(f, b, i)
			}
		}
	}
	prune(f)
}

// raiseRuntime appends to b the call of .raise for the run-time error in.
func (l *lowerer) raiseRuntime(f *ir.Func, b *ir.Block, in *ir.Instr) {
	pos := in.Pos
	x, y := f.Const(b, 0, pos), f.Const(b, 0, pos)
	signed := f.BoolConst(b, false, pos)
	mp, ml := f.TypedConst(b, ir.Ptr, 0, pos), f.Const(b, 0, pos)
	switch {
	case in.Index == ir.PanicMsg:
		mp, ml = in.Args[0], in.Args[1]
	case ir.IsBounds(in.Index):
		x, y = in.Args[0], in.Args[1]
		signed = f.BoolConst(b, x.Type.Signed(), pos)
		if x.Type != ir.I64 {
			x = f.Conv(b, ir.I64, x, pos)
		}
	}
	kind := f.Const(b, kindRuntime+int64(in.Index), pos)
	f.Call(b, l.ps.raise, []*ir.Instr{kind, x, y, signed, mp, ml}, pos)
}

// splitAfter ends b after the call at index i, and the extracts of its
// results, which move up to follow it, with a check of .unwinding that
// goes to f's exit, or else continues in a new block with the rest of b.
// The values of b that the rest uses are carried in slots.
func (l *lowerer) splitAfter(f *ir.Func, b *ir.Block, i int) {
	call := b.Instrs[i]
	head := append([]*ir.Instr(nil), b.Instrs[:i+1]...)
	var rest []*ir.Instr
	for _, in := range b.Instrs[i+1:] {
		if in.Op == ir.Extract && in.Args[0] == call {
			head = append(head, in)
		} else {
			rest = append(rest, in)
		}
	}
	defined := map[*ir.Instr]bool{}
	for _, in := range head {
		defined[in] = true
	}
	pos := call.Pos
	next := f.NewBlock()
	b.Instrs = head
	carried := map[*ir.Instr]*ir.Instr{}
	for _, in := range rest {
		for j, a := range in.Args {
			if !defined[a] {
				continue
			}
			if carried[a] == nil {
				s := f.NewSlot("tmp", a.Type, pos)
				f.Store(b, s, a, pos)
				carried[a] = f.Load(next, s, pos)
			}
			in.Args[j] = carried[a]
		}
	}
	f.Branch(b, f.GLoad(b, l.ps.unwinding, pos), l.exit(f), next, pos)
	next.Instrs = append(next.Instrs, rest...)
}
