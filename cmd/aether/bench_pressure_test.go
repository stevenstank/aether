package main

// Register-pressure analysis of SSA functions for the benchmark harness
// (BENCHMARKS.md): liveness of SSA values, the most values live at once,
// and the values live across calls, statically and weighted by measured
// block frequencies. It is a measurement of the IR only; nothing here
// allocates registers.

import (
	"maps"

	"github.com/stevenstank/aether/internal/ir"
)

// pressure is the register-pressure analysis of one SSA function.
type pressure struct {
	maxLive, maxLiveNC int // most simultaneously live values; NC: constants excluded
	values             int
	crossing           int // values live across at least one call
	crossingNC         int
	maxAcrossCall      int // most values live across one call
	phis               int
	// weighted by block frequency, when known
	hist         map[int]int64 // instruction executions by live count (constants excluded)
	callCrossDyn int64         // sum over executed calls of values live across them (constants excluded)
	callsDyn     int64
	instrExecs   int64
	// twoAddrDyn models the register copies x86's two-address forms and
	// fixed registers would need if every value were in a register:
	// a copy when an overwritten operand is still live after the
	// instruction, the dividend into %rax and the result out of it, and
	// the shift count into %cl.
	twoAddrDyn int64
}

// copies is twoAddrDyn's cost of x, given the values live after it.
func copies(x *ir.Instr, after map[*ir.Instr]bool) int64 {
	live := func(i int) bool { return after[x.Args[i]] && x.Args[i].Op != ir.Const }
	switch x.Op {
	case ir.Add, ir.Mul, ir.And, ir.Or, ir.Xor:
		if live(0) && live(1) {
			return 1
		}
	case ir.Sub, ir.AndNot, ir.Neg, ir.Com, ir.Not:
		if live(0) {
			return 1
		}
	case ir.Div, ir.Rem:
		return 2
	case ir.Shl, ir.Shr:
		n := int64(1)
		if live(0) {
			n++
		}
		return n
	}
	return 0
}

// width is the number of 64-bit words value v occupies.
func width(v *ir.Instr) int {
	if v.Type == ir.Tuple {
		return len(v.Callee.Results)
	}
	return 1
}

// analyze computes value liveness of the SSA function f and its pressure.
// A φ operand is live out of its edge's source block. freq gives each
// block's execution count, or is nil.
func analyze(f *ir.Func, freq []int64) pressure {
	nb := len(f.Blocks)
	succ := make([][]*ir.Block, nb)
	for _, b := range f.Blocks {
		succ[b.ID] = b.Instrs[len(b.Instrs)-1].Targets
	}
	type set = map[*ir.Instr]bool
	liveIn := make([]set, nb)
	liveOut := make([]set, nb)
	for i := range nb {
		liveIn[i], liveOut[i] = set{}, set{}
	}
	isVal := func(v *ir.Instr) bool { return v.ID >= 0 }
	// phiUses[b] are the values b's out-edges feed to phis.
	phiUses := make([]set, nb)
	for i := range nb {
		phiUses[i] = set{}
	}
	p := pressure{hist: map[int]int64{}}
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if isVal(in) {
				p.values++
			}
			if in.Op == ir.Phi {
				p.phis++
				for i, a := range in.Args {
					phiUses[in.Edges[i].From.ID][a] = true
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for i := nb - 1; i >= 0; i-- {
			b := f.Blocks[i]
			out := set{}
			for _, s := range succ[b.ID] {
				for v := range liveIn[s.ID] {
					out[v] = true
				}
			}
			for v := range phiUses[b.ID] {
				out[v] = true
			}
			in := maps.Clone(out)
			for j := len(b.Instrs) - 1; j >= 0; j-- {
				x := b.Instrs[j]
				delete(in, x)
				if x.Op != ir.Phi {
					for _, a := range x.Args {
						in[a] = true
					}
				}
			}
			// The sets only grow, so equal sizes mean equal sets.
			if len(out) != len(liveOut[b.ID]) || len(in) != len(liveIn[b.ID]) {
				changed = true
			}
			liveOut[b.ID], liveIn[b.ID] = out, in
		}
	}
	count := func(s set, noConst bool) (n int) {
		for v := range s {
			if noConst && v.Op == ir.Const {
				continue
			}
			n += width(v)
		}
		return n
	}
	crossing, crossingNC := set{}, set{}
	for _, b := range f.Blocks {
		var fq int64 = -1
		if freq != nil {
			fq = freq[b.ID]
		}
		live := maps.Clone(liveOut[b.ID])
		for j := len(b.Instrs) - 1; j >= 0; j-- {
			x := b.Instrs[j]
			// after is the set live after x.
			after := live
			if x.Op == ir.Call {
				var n int
				for v := range after {
					if v != x {
						crossing[v] = true
						if v.Op != ir.Const {
							crossingNC[v] = true
							n += width(v)
						}
					}
				}
				p.maxAcrossCall = max(p.maxAcrossCall, n)
				if fq > 0 {
					p.callCrossDyn += fq * int64(n)
					p.callsDyn += fq
				}
			}
			if fq > 0 {
				p.twoAddrDyn += fq * copies(x, after)
			}
			live = maps.Clone(after)
			delete(live, x)
			if x.Op != ir.Phi {
				for _, a := range x.Args {
					live[a] = true
				}
			}
			// The pressure at x: what is live before it plus its result.
			at := maps.Clone(live)
			if isVal(x) {
				at[x] = true
			}
			p.maxLive = max(p.maxLive, count(at, false))
			nc := count(at, true)
			p.maxLiveNC = max(p.maxLiveNC, nc)
			if fq > 0 && x.Op != ir.Phi {
				p.hist[nc] += fq
				p.instrExecs += fq
			}
		}
	}
	p.crossing, p.crossingNC = len(crossing), len(crossingNC)
	return p
}
