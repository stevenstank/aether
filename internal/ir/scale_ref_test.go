package ir

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// The verifier's definite-assignment check and promotion's phi placement
// were once dense fixed points over blocks times slots, iterated until
// nothing changed. They are kept here as references for the sparse versions,
// which must give the same answers.

// refDefinitelyStored is the old fixed point: in(entry) is empty, every other
// block starts with all slots, and in(b) is the intersection of out over b's
// reachable predecessors, out(b) = in(b) ∪ stores(b).
func refDefinitelyStored(v *verifier, reachable []bool) [][]bool {
	n, ns := len(v.f.Blocks), len(v.f.Slots)
	in, stores, preds := make([][]bool, n), make([][]bool, n), make([][]int, n)
	for i, b := range v.f.Blocks {
		in[i], stores[i] = make([]bool, ns), make([]bool, ns)
		for j := range in[i] {
			in[i][j] = i != 0
		}
		for _, inst := range b.Instrs {
			if inst != nil && inst.Op == Store && v.isSlot(inst.Slot) {
				stores[i][inst.Slot.ID] = true
			}
		}
		if reachable[i] {
			for _, s := range v.successors(b) {
				preds[s] = append(preds[s], i)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for i := 1; i < n; i++ {
			if !reachable[i] {
				continue
			}
			for j := range ns {
				all := true
				for _, p := range preds[i] {
					all = all && (in[p][j] || stores[p][j])
				}
				if in[i][j] && !all {
					in[i][j] = false
					changed = true
				}
			}
		}
	}
	return in
}

// refUnstored is the set of loads the old check rejected.
func refUnstored(f *Func) map[*Instr]bool {
	v := &verifier{f: f}
	reachable := v.reachable()
	in := refDefinitelyStored(v, reachable)
	bad := map[*Instr]bool{}
	for i, b := range f.Blocks {
		stored := slices.Clone(in[i])
		for _, x := range b.Instrs {
			switch x.Op {
			case Store:
				stored[x.Slot.ID] = true
			case Load:
				if !stored[x.Slot.ID] {
					bad[x] = true
				}
			}
		}
	}
	return bad
}

// refPhis is the old placement: liveness as a dense fixed point over all
// blocks, then the pruned iterated frontier. It returns, by block, the slots
// that get phis, in order.
func refPhis(f *Func, d *DomTree) [][]int {
	n, ns := len(f.Blocks), len(f.Slots)
	upward, stores := make([][]bool, n), make([][]bool, n)
	for _, b := range f.Blocks {
		upward[b.ID], stores[b.ID] = make([]bool, ns), make([]bool, ns)
		for _, in := range b.Instrs {
			switch in.Op {
			case Load:
				upward[b.ID][in.Slot.ID] = upward[b.ID][in.Slot.ID] || !stores[b.ID][in.Slot.ID]
			case Store:
				stores[b.ID][in.Slot.ID] = true
			}
		}
	}
	live := make([][]bool, n)
	for i := range live {
		live[i] = slices.Clone(upward[i])
	}
	for changed := true; changed; {
		changed = false
		for i := n - 1; i >= 0; i-- {
			for _, succ := range Succs(f.Blocks[i]) {
				for s := range ns {
					if live[succ.ID][s] && !stores[i][s] && !live[i][s] {
						live[i][s] = true
						changed = true
					}
				}
			}
		}
	}
	out := make([][]int, n)
	for s := range ns {
		has, queued := make([]bool, n), make([]bool, n)
		var work []*Block
		for _, b := range f.Blocks {
			if stores[b.ID][s] {
				queued[b.ID] = true
				work = append(work, b)
			}
		}
		for len(work) > 0 {
			x := work[len(work)-1]
			work = work[:len(work)-1]
			for _, y := range d.Frontier(x) {
				if has[y.ID] || !live[y.ID][s] {
					continue
				}
				has[y.ID] = true
				out[y.ID] = append(out[y.ID], s)
				if !queued[y.ID] {
					queued[y.ID] = true
					work = append(work, y)
				}
			}
		}
	}
	return out
}

// randomSlots builds a random ordinary function main: up to 14 blocks with
// random jumps, branches, and returns, and random loads and stores of up to
// five slots. With entryFree, no edge enters the entry block.
func randomSlots(r *rand.Rand, entryFree bool) (*Program, *Func) {
	p := &Program{}
	f := p.NewFunc("main", "p.go", Pos{1, 6})
	n, ns := 1+r.IntN(14), 1+r.IntN(5)
	for range n - 1 {
		f.NewBlock()
	}
	var slots []*Slot
	for i := range ns {
		slots = append(slots, f.NewSlot(fmt.Sprintf("x%d", i), I64, Pos{2, 2}))
	}
	target := func() *Block {
		if entryFree && n > 1 {
			return f.Blocks[1+r.IntN(n-1)]
		}
		return f.Blocks[r.IntN(n)]
	}
	for _, b := range f.Blocks {
		for range r.IntN(5) {
			s := slots[r.IntN(ns)]
			if r.IntN(2) == 0 {
				f.Store(b, s, f.Const(b, int64(r.IntN(9)), at), at)
			} else {
				f.Load(b, s, at)
			}
		}
		switch r.IntN(4) {
		case 0:
			f.Ret(b, at)
		case 1:
			f.Jump(b, target(), at)
		default:
			f.Branch(b, f.BoolConst(b, r.IntN(2) == 0, at), target(), target(), at)
		}
	}
	return p, f
}

// TestUnstoredLoadsMatchReference checks the verifier's definite-assignment
// check against the old fixed point on random functions, unreachable blocks
// and edges into the entry included: both must reject exactly the same loads.
func TestUnstoredLoadsMatchReference(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	rejected := 0
	for i := range 3000 {
		p, f := randomSlots(r, i%2 == 0)
		want := refUnstored(f)
		got := map[*Instr]bool{}
		for _, err := range Verify(p) {
			e := err.(*VerifyError)
			if strings.Contains(e.Msg, "before any store") {
				got[f.Blocks[e.Block].Instrs[e.Index]] = true
			}
		}
		if len(got) != len(want) {
			t.Fatalf("function %d: %d loads rejected, the reference rejects %d:\n%s", i, len(got), len(want), p)
		}
		for x := range want {
			if !got[x] {
				t.Fatalf("function %d: %s not rejected, the reference rejects it:\n%s", i, x, p)
			}
		}
		rejected += len(want)
	}
	if rejected == 0 {
		t.Fatal("no function has a load to reject")
	}
	t.Logf("%d loads rejected by both", rejected)
}

// TestPlacePhisMatchesReference checks the phi placement of Promote against
// the old placement on the random functions that Verify accepts and Promote
// takes.
func TestPlacePhisMatchesReference(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 5))
	checked, phis := 0, 0
	for checked < 1500 {
		p, f := randomSlots(r, true)
		if len(Verify(p)) > 0 {
			continue
		}
		d := Dominators(f)
		want := refPhis(f, d)
		got, slotOf := placePhis(f, d)
		for b := range f.Blocks {
			var slots []int
			for _, phi := range got[b] {
				slots = append(slots, slotOf[phi])
			}
			if !slices.Equal(slots, want[b]) {
				t.Fatalf("b%d: phis of slots %v, the reference places %v:\n%s", b, slots, want[b], p)
			}
			phis += len(slots)
		}
		checked++
	}
	if phis == 0 {
		t.Fatal("no function needs a phi")
	}
	t.Logf("%d functions, %d phis placed by both", checked, phis)
}

// TestVerifyFuncs checks that VerifyFuncs reports the violations of the
// functions it is given, as Verify does, and nothing of the others.
func TestVerifyFuncs(t *testing.T) {
	p := &Program{}
	good := p.NewFunc("main", "p.go", Pos{1, 6})
	good.Ret(good.Entry(), at)
	bad := p.NewFunc("g", "p.go", Pos{2, 6})
	bad.Println(bad.Entry(), nil, at) // no terminator
	if errs := VerifyFuncs(p, []*Func{good}); len(errs) != 0 {
		t.Errorf("well-formed function rejected: %v", errs)
	}
	want := fmt.Sprint(Verify(p))
	if got := fmt.Sprint(VerifyFuncs(p, []*Func{good, bad})); got != want {
		t.Errorf("VerifyFuncs = %s, Verify = %s", got, want)
	}
	other := (&Program{}).NewFunc("h", "p.go", Pos{3, 6})
	other.Ret(other.Entry(), at)
	if errs := VerifyFuncs(p, []*Func{other}); len(errs) != 1 || !strings.Contains(errs[0].Error(), "not a function of the program") {
		t.Errorf("function of another program: %v", errs)
	}
}
