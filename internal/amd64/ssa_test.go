package amd64

import (
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
