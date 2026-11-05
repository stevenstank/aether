package interp

import (
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// TestPhiSimultaneous checks that the phis of a block take their values
// together (D-025): a loop that swaps a and b through two phis, each
// naming the other, must not see a value written by the first.
//
//	a, b := 1, 2
//	for i := 0; i < 3; i++ { println(a, b); a, b = b, a+b }
func TestPhiSimultaneous(t *testing.T) {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	f.SSA = true
	b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	zero, one, two := f.Const(b0, 0, p1), f.Const(b0, 1, p1), f.Const(b0, 2, p1)
	f.Jump(b0, b1, p1)
	a, b, i := f.Phi(b1, ir.I64, p1), f.Phi(b1, ir.I64, p1), f.Phi(b1, ir.I64, p1)
	f.Branch(b1, f.Compare(b1, ir.Lt, i, f.Const(b1, 3, p1), p1), b2, b3, p1)
	f.Println(b2, []*ir.Instr{a, b}, p1)
	sum := f.Binary(b2, ir.Add, a, b, p1)
	inc := f.Binary(b2, ir.Add, i, one, p1)
	f.Jump(b2, b1, p1)
	f.Ret(b3, p1)
	a.AddIncoming(ir.Edge{From: b0, Index: 0}, one)
	a.AddIncoming(ir.Edge{From: b2, Index: 0}, b) // the old b
	b.AddIncoming(ir.Edge{From: b0, Index: 0}, two)
	b.AddIncoming(ir.Edge{From: b2, Index: 0}, sum)
	i.AddIncoming(ir.Edge{From: b0, Index: 0}, zero)
	i.AddIncoming(ir.Edge{From: b2, Index: 0}, inc)
	if got, want := output(t, p), "1 2\n2 3\n3 5\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestPhiParallelEdges checks that a phi takes the value of the edge taken,
// not of the predecessor block: both targets of the branch are b1.
func TestPhiParallelEdges(t *testing.T) {
	for _, cond := range []bool{true, false} {
		p := &ir.Program{}
		f := p.NewFunc("main", "p.go", p1)
		f.SSA = true
		b0, b1 := f.Entry(), f.NewBlock()
		x, y := f.Const(b0, 10, p1), f.Const(b0, 20, p1)
		f.Branch(b0, f.BoolConst(b0, cond, p1), b1, b1, p1)
		r := f.Phi(b1, ir.I64, p1)
		r.AddIncoming(ir.Edge{From: b0, Index: 0}, x)
		r.AddIncoming(ir.Edge{From: b0, Index: 1}, y)
		f.Println(b1, []*ir.Instr{r}, p1)
		f.Ret(b1, p1)
		want := "20\n"
		if cond {
			want = "10\n"
		}
		if got := output(t, p); got != want {
			t.Errorf("cond %v: got %q, want %q", cond, got, want)
		}
	}
}
