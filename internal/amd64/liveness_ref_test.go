package amd64

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// The allocator's liveness was a fixed point over map sets, iterated over
// the blocks until nothing changed, with a map per instruction for the
// values live after it. It is kept here as the reference that liveSets must
// match, and the checker sweep uses it as liveness independent of the
// allocator's.

// refLiveness, the allocator's liveness before liveSets, computes the values live out of each block of the SSA function
// f, by block ID. A φ operand is live out of its edge's source block, not
// live into the φ's block.
func refLiveness(f *ir.Func) []valueSet {
	n := len(f.Blocks)
	liveIn, liveOut := make([]valueSet, n), make([]valueSet, n)
	phiUses := make([]valueSet, n)
	for i := range n {
		liveIn[i], liveOut[i], phiUses[i] = valueSet{}, valueSet{}, valueSet{}
	}
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if in.Op == ir.Phi {
				for i, a := range in.Args {
					phiUses[in.Edges[i].From.ID][a] = true
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for i := n - 1; i >= 0; i-- {
			b := f.Blocks[i]
			out := valueSet{}
			for _, s := range ir.Succs(b) {
				for v := range liveIn[s.ID] {
					out[v] = true
				}
			}
			for v := range phiUses[b.ID] {
				out[v] = true
			}
			in := refLiveBefore(b, 0, out)
			// The sets only grow, so equal sizes mean equal sets.
			if len(out) != len(liveOut[b.ID]) || len(in) != len(liveIn[b.ID]) {
				changed = true
			}
			liveIn[b.ID], liveOut[b.ID] = in, out
		}
	}
	return liveOut
}

// refLiveBefore returns the values live before instruction i of b, given those
// live out of b. Before a φ means at the block's entry, where φs are
// defined and their operands are not used.
func refLiveBefore(b *ir.Block, i int, out valueSet) valueSet {
	live := valueSet{}
	for v := range out {
		live[v] = true
	}
	for j := len(b.Instrs) - 1; j >= i; j-- {
		x := b.Instrs[j]
		delete(live, x)
		if x.Op != ir.Phi {
			for _, a := range x.Args {
				live[a] = true
			}
		}
	}
	return live
}

// refLiveAfter returns, for each instruction of b, the values live after it.
func refLiveAfter(b *ir.Block, out valueSet) []valueSet {
	after := make([]valueSet, len(b.Instrs))
	live := valueSet{}
	for v := range out {
		live[v] = true
	}
	for j := len(b.Instrs) - 1; j >= 0; j-- {
		x := b.Instrs[j]
		after[j] = valueSet{}
		for v := range live {
			after[j][v] = true
		}
		delete(live, x)
		if x.Op != ir.Phi {
			for _, a := range x.Args {
				live[a] = true
			}
		}
	}
	return after
}

// randomSSA builds a random SSA function main by promoting a random ordinary
// one: up to 16 blocks with random jumps and branches on comparisons of
// slots, every slot stored in the entry first, random stores of sums and
// constants, and a println of every slot before each return. It returns nil
// if the CFG leaves a block unreachable.
func randomSSA(r *rand.Rand) *ir.Func {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	n, ns := 1+r.IntN(16), 1+r.IntN(6)
	for range n - 1 {
		f.NewBlock()
	}
	var slots []*ir.Slot
	for i := range ns {
		s := f.NewSlot(fmt.Sprintf("x%d", i), ir.I64, p1)
		slots = append(slots, s)
		f.Store(f.Entry(), s, f.Const(f.Entry(), int64(i), p1), p1)
	}
	slot := func() *ir.Slot { return slots[r.IntN(ns)] }
	target := func() *ir.Block { return f.Blocks[1+r.IntN(n-1)] }
	for _, b := range f.Blocks {
		for range r.IntN(4) {
			if r.IntN(2) == 0 {
				f.Store(b, slot(), f.Binary(b, ir.Add, f.Load(b, slot(), p1), f.Load(b, slot(), p1), p1), p1)
			} else {
				f.Store(b, slot(), f.Const(b, int64(r.IntN(9)), p1), p1)
			}
		}
		switch k := r.IntN(4); {
		case n == 1 || k == 0:
			var vs []*ir.Instr
			for _, s := range slots {
				vs = append(vs, f.Load(b, s, p1))
			}
			f.Println(b, vs, p1)
			f.Ret(b, p1)
		case k == 1:
			f.Jump(b, target(), p1)
		default:
			f.Branch(b, f.Compare(b, ir.Lt, f.Load(b, slot(), p1), f.Load(b, slot(), p1), p1), target(), target(), p1)
		}
	}
	if len(ir.Verify(p)) > 0 {
		return nil
	}
	if err := ir.Promote(f); err != nil {
		panic(err)
	}
	return f
}

// TestLivenessMatchesReference checks liveSets against the reference on the
// test functions and on random SSA functions: the values live into and out
// of each reachable block and after each instruction must be the same.
func TestLivenessMatchesReference(t *testing.T) {
	var fs []*ir.Func
	for name := range raFuncs {
		_, f := raBuild(t, name)
		fs = append(fs, f)
	}
	r := rand.New(rand.NewPCG(5, 9))
	for len(fs) < 2000 {
		if f := randomSSA(r); f != nil {
			fs = append(fs, f)
		}
	}
	sets := func(lv *liveSets, s []uint64) valueSet {
		vs := valueSet{}
		lv.each(s, func(v *ir.Instr) { vs[v] = true })
		return vs
	}
	same := func(a, b valueSet) bool {
		if len(a) != len(b) {
			return false
		}
		for v := range a {
			if !b[v] {
				return false
			}
		}
		return true
	}
	checked := 0
	for _, f := range fs {
		d := ir.Dominators(f)
		lv, refOut := liveness(f, d), refLiveness(f)
		for _, b := range f.Blocks {
			if !d.Reachable(b) {
				continue
			}
			if !same(sets(lv, lv.liveOut(b)), refOut[b.ID]) {
				t.Fatalf("func %s: b%d: live out %v, reference %v", f.Name, b.ID, sortedValues(sets(lv, lv.liveOut(b))), sortedValues(refOut[b.ID]))
			}
			if !same(sets(lv, lv.liveIn(b)), refLiveBefore(b, 0, refOut[b.ID])) {
				t.Fatalf("func %s: b%d: live in differs from the reference", f.Name, b.ID)
			}
			after, _ := lv.afterSets(b, nil)
			for j, want := range refLiveAfter(b, refOut[b.ID]) {
				if !same(sets(lv, after[j]), want) {
					t.Fatalf("func %s: b%d: live after instruction %d differs from the reference", f.Name, b.ID, j)
				}
			}
			checked++
		}
	}
	t.Logf("%d functions, %d blocks", len(fs), checked)
}
