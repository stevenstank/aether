package lower

import (
	"slices"

	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/ir"
)

// Cooperative preemption at loop back edges (D-059). Each iteration of a
// for or range loop, and each backward goto, of the program's and the
// standard library's code begins with a Preempt, so that a goroutine that
// computes without switching still lets the others, the timers, the
// poller, and the cleanups run once its time slice is over. Aether's
// overlay files, whose runtime code relies on running without a switch
// between two of its statements (D-049), have none, nor have the
// functions lowering generates: the scheduler, channels, maps.
//
// Every function of that code also begins with a Preempt, so that
// recursion with no loop is preempted too (D-060); the backend makes it in
// the prologue's stack check, at no cost.
//
// A program without a scheduler has nothing to switch to: its Preempts
// are removed. Otherwise Init, or main without one, first registers the
// scheduler's .preempt (SysPreempt), which yields as Gosched does once
// the running time slice is over, measured as the switches measure it,
// unless it is called from code the switch hooks run (.inhooks): the
// scheduler is never re-entered.

// backEdge adds a Preempt at the head of a loop being lowered, or before a
// backward goto, unless the function is an overlay file's.
func (l *lowerer) backEdge(pos ir.Pos) {
	if frontend.IsOverlay(l.fn.File) {
		return
	}
	l.fn.Preempt(l.b, pos)
	l.preempts = true
}

// entryPreempt adds a Preempt as the first instruction of the function
// being lowered, before a closure's context param, which the backend makes
// in the prologue (D-060).
func (l *lowerer) entryPreempt(pos ir.Pos) {
	entry := l.fn.Entry()
	n := len(entry.Instrs)
	l.backEdge(pos)
	entry.Instrs = append(entry.Instrs[n:], entry.Instrs[:n]...)
}

// preemption registers the scheduler's preemption, or removes the Preempts
// of a program without a scheduler.
func (l *lowerer) preemption(prog *ir.Program) {
	if !l.preempts {
		return
	}
	if l.sch == nil {
		for _, f := range prog.Funcs {
			for _, b := range f.Blocks {
				b.Instrs = slices.DeleteFunc(b.Instrs, func(in *ir.Instr) bool { return in.Op == ir.Preempt })
			}
		}
		return
	}
	sc := l.sch
	fn := l.genFunc("preempt", nil, nil, func(_ []func() *ir.Instr, pos ir.Pos) {
		m := mb{l, pos}
		if sc.inHooks != nil {
			// Not from code the switch hooks call (chan.go).
			out := m.when(l.fn.GLoad(l.b, sc.inHooks, pos))
			l.fn.Ret(l.b, pos)
			l.b = out
		}
		ran := m.bin(ir.Sub, l.ticks(pos), l.fn.GLoad(l.b, sc.sliceStart, pos))
		within := m.when(m.cmp(ir.Ge, ran, l.fn.TypedConst(l.b, ir.U64, sliceTicks, pos)))
		l.yield(pos)
		m.join(within)
		l.fn.Ret(l.b, pos)
	})
	l.buildWrappers()
	at := prog.Init
	if at == nil {
		for _, f := range prog.Funcs {
			if f.Name == "main" {
				at = f
			}
		}
	}
	if at == nil {
		l.fail(nil, "no function to register the preemption in")
	}
	// The registration goes first: it is built at the end of the entry
	// block, then moved, with no param before it (Init and main have
	// none).
	entry := at.Entry()
	n := len(entry.Instrs)
	pos := at.Pos
	at.Sys(entry, ir.SysPreempt, ir.NoType, pos, at.FuncAddr(entry, fn, pos))
	entry.Instrs = append(entry.Instrs[n:], entry.Instrs[:n]...)
}
