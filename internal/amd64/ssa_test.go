package amd64

import (
	"fmt"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// TestSSACrossBlockValues runs SSA functions without phis (D-025), whose
// values are used in blocks their definitions dominate, in the interpreter
// and natively. Slot promotion (D-006 part 3) depends on both executing
// such uses correctly. f is
//
//	func f(p int) int {
//		n := -p
//		if p < 0 { println(p, n*n) } else { println(n) }
//		return p + n
//	}
//
// with p and n defined in the entry block and used in each arm and in the
// join; main calls it on both arms and uses the results in a later block.
func TestSSACrossBlockValues(t *testing.T) {
	p := &ir.Program{}
	m := p.NewFunc("main", "p.go", p1)
	f := p.NewFunc("f", "p.go", p1)
	f.Params, f.Results = []ir.Type{ir.I64}, []ir.Type{ir.I64}
	m.SSA, f.SSA = true, true

	b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	x := f.Param(b0, 0, p1)
	n := f.Neg(b0, x, p1)
	f.Branch(b0, f.Compare(b0, ir.Lt, x, f.Const(b0, 0, p1), p1), b1, b2, p1)
	f.Println(b1, []*ir.Instr{x, f.Binary(b1, ir.Mul, n, n, p1)}, p1)
	f.Jump(b1, b3, p1)
	f.Println(b2, []*ir.Instr{n}, p1)
	f.Jump(b2, b3, p1)
	f.Ret(b3, p1, f.Binary(b3, ir.Add, x, n, p1))

	e, next := m.Entry(), m.NewBlock()
	neg := m.Const(e, -3, p1)
	r1 := m.Call(e, f, []*ir.Instr{neg}, p1)
	r2 := m.Call(e, f, []*ir.Instr{m.Const(e, 4, p1)}, p1)
	m.Jump(e, next, p1)
	m.Println(next, []*ir.Instr{r1, r2, neg}, p1)
	m.Ret(next, p1)

	if errs := ir.Verify(p); len(errs) != 0 {
		t.Fatalf("Verify: %v\n%s", errs, p)
	}
	for _, fn := range p.Funcs {
		for _, b := range fn.Blocks {
			for _, in := range b.Instrs {
				if in.Op == ir.Phi || in.Op == ir.Load || in.Op == ir.Store {
					t.Fatalf("func %s has %s", fn.Name, in.Op)
				}
			}
		}
	}
	runBoth(t, p, "-3 9\n-4\n0 0 -3\n")
}

// TestNativePhiCopies checks the parallel copy on a loop's back edge,
// natively and in the interpreter. The phis of the loop header read each
// other: a and b swap, c, d, and e rotate (two cycles, which need %rcx), f
// copies a's old value (a fan-out from a phi that is itself overwritten),
// and g keeps its own value (a copy omitted). The function is
//
//	a, b, c, d, e, f, g := 1, 2, 3, 4, 5, 6, 7
//	for i := 0; i < 4; i++ {
//		println(a, b, c, d, e, f, g)
//		a, b, c, d, e, f = b, a, d, e, c, a
//	}
//	println(a, b, c, d, e, f, g)
func TestNativePhiCopies(t *testing.T) {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	f.SSA = true
	b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	var init []*ir.Instr
	for v := range int64(7) {
		init = append(init, f.Const(b0, v+1, p1))
	}
	zero, one := f.Const(b0, 0, p1), f.Const(b0, 1, p1)
	f.Jump(b0, b1, p1)
	var phis []*ir.Instr
	for range 8 {
		phis = append(phis, f.Phi(b1, ir.I64, p1))
	}
	a, b, c, d, e, g, i := phis[0], phis[1], phis[2], phis[3], phis[4], phis[6], phis[7]
	f.Branch(b1, f.Compare(b1, ir.Lt, i, f.Const(b1, 4, p1), p1), b2, b3, p1)
	f.Println(b2, phis[:7], p1)
	inc := f.Binary(b2, ir.Add, i, one, p1)
	f.Jump(b2, b1, p1)
	f.Println(b3, phis[:7], p1)
	f.Ret(b3, p1)
	back := []*ir.Instr{b, a, d, e, c, a, g, inc}
	for k, phi := range phis {
		phi.AddIncoming(ir.Edge{From: b0, Index: 0}, append(init, zero)[k])
		phi.AddIncoming(ir.Edge{From: b2, Index: 0}, back[k])
	}
	want := "1 2 3 4 5 6 7\n2 1 4 5 3 1 7\n1 2 5 3 4 2 7\n2 1 3 4 5 1 7\n1 2 4 5 3 2 7\n"
	runBoth(t, p, want)
}

// TestNativePhiEdges checks phis reached along critical and parallel edges,
// for every shape of branch: only the true target has phis, only the false
// target, both, and both targets the same block. The function, for
// conditions c1 and c2, is
//
//	b0: branch c1, b1, b2     (or branch !c1, b2, b1)
//	b1: branch c2, b2, b3
//	b2: x = phi(b0: 10, b1: 20); println(x); jump b3
//	b3: y = phi(b1: 30, b2: x); println(y); ret
//
// and in the parallel case b0 branches on c1 to b1 along both edges, where
// a phi takes 10 or 20 by the edge.
func TestNativePhiEdges(t *testing.T) {
	for _, swap := range []bool{false, true} {
		for _, c1 := range []bool{false, true} {
			for _, c2 := range []bool{false, true} {
				p := &ir.Program{}
				f := p.NewFunc("main", "p.go", p1)
				f.SSA = true
				b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
				ten, twenty, thirty := f.Const(b0, 10, p1), f.Const(b0, 20, p1), f.Const(b0, 30, p1)
				cond := f.BoolConst(b0, c1, p1)
				// The edge from b0 to b2.
				e02 := ir.Edge{From: b0, Index: 1}
				if swap {
					f.Branch(b0, f.Not(b0, cond, p1), b2, b1, p1)
					e02.Index = 0
				} else {
					f.Branch(b0, cond, b1, b2, p1)
				}
				f.Branch(b1, f.BoolConst(b1, c2, p1), b2, b3, p1)
				x := f.Phi(b2, ir.I64, p1)
				f.Println(b2, []*ir.Instr{x}, p1)
				f.Jump(b2, b3, p1)
				y := f.Phi(b3, ir.I64, p1)
				f.Println(b3, []*ir.Instr{y}, p1)
				f.Ret(b3, p1)
				x.AddIncoming(e02, ten)
				x.AddIncoming(ir.Edge{From: b1, Index: 0}, twenty)
				y.AddIncoming(ir.Edge{From: b1, Index: 1}, thirty)
				y.AddIncoming(ir.Edge{From: b2, Index: 0}, x)
				want := "10\n10\n"
				switch {
				case c1 && c2:
					want = "20\n20\n"
				case c1:
					want = "30\n"
				}
				t.Run(fmt.Sprintf("swap=%v,c1=%v,c2=%v", swap, c1, c2), func(t *testing.T) { runBoth(t, p, want) })
			}
		}
	}
	for _, c := range []bool{false, true} {
		p := &ir.Program{}
		f := p.NewFunc("main", "p.go", p1)
		f.SSA = true
		b0, b1 := f.Entry(), f.NewBlock()
		x, y := f.Const(b0, 10, p1), f.Const(b0, 20, p1)
		f.Branch(b0, f.BoolConst(b0, c, p1), b1, b1, p1)
		r, s := f.Phi(b1, ir.I64, p1), f.Phi(b1, ir.I64, p1)
		r.AddIncoming(ir.Edge{From: b0, Index: 0}, x)
		r.AddIncoming(ir.Edge{From: b0, Index: 1}, y)
		s.AddIncoming(ir.Edge{From: b0, Index: 0}, y)
		s.AddIncoming(ir.Edge{From: b0, Index: 1}, x)
		f.Println(b1, []*ir.Instr{r, s}, p1)
		f.Ret(b1, p1)
		want := "20 10\n"
		if c {
			want = "10 20\n"
		}
		t.Run(fmt.Sprintf("parallel,c=%v", c), func(t *testing.T) { runBoth(t, p, want) })
	}
}

// TestNativePhiLoopExit checks that the copies for a branch's back edge are
// made only along it: the loop below exits from the block whose phi the
// back edge assigns, and the exit uses that phi, which must keep its value.
//
//	i := 0
//	for { println(i); n := i + 1; if !(n < 3) { println(i, n); return }; i = n }
func TestNativePhiLoopExit(t *testing.T) {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	f.SSA = true
	b0, b1, b2 := f.Entry(), f.NewBlock(), f.NewBlock()
	zero := f.Const(b0, 0, p1)
	f.Jump(b0, b1, p1)
	i := f.Phi(b1, ir.I64, p1)
	f.Println(b1, []*ir.Instr{i}, p1)
	n := f.Binary(b1, ir.Add, i, f.Const(b1, 1, p1), p1)
	f.Branch(b1, f.Compare(b1, ir.Lt, n, f.Const(b1, 3, p1), p1), b1, b2, p1)
	i.AddIncoming(ir.Edge{From: b0, Index: 0}, zero)
	i.AddIncoming(ir.Edge{From: b1, Index: 0}, n)
	f.Println(b2, []*ir.Instr{i, n}, p1)
	f.Ret(b2, p1)
	runBoth(t, p, "0\n1\n2\n2 3\n")
}
