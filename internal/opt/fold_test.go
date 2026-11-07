package opt

import (
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

var at = ir.Pos{Line: 6, Col: 2}

// The folding fixtures are ordinary functions main, built as lowering would
// build them, promoted, and then folded; want is the folded form.
var foldFixtures = []struct {
	name  string
	build func(p *ir.Program, f *ir.Func)
	want  string
}{
	{
		// x := 1; if true { x = 2; println(3) }; println(x): the branch
		// folds, b2 is removed, and the phi at the join has one value left.
		name: "diamond",
		build: func(_ *ir.Program, f *ir.Func) {
			x, y := f.NewSlot("x", ir.I64, ir.Pos{Line: 2, Col: 2}), f.NewSlot("y", ir.I64, ir.Pos{Line: 3, Col: 2})
			b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
			f.Store(b0, x, f.Const(b0, 1, at), at)
			f.Branch(b0, f.BoolConst(b0, true, at), b1, b2, at)
			f.Store(b1, x, f.Const(b1, 2, at), at)
			f.Store(b1, y, f.Const(b1, 3, at), at)
			f.Println(b1, []*ir.Instr{f.Load(b1, y, at)}, at)
			f.Jump(b1, b3, at)
			f.Jump(b2, b3, at)
			f.Println(b3, []*ir.Instr{f.Load(b3, x, at)}, at)
			f.Ret(b3, at)
		},
		want: `func main ssa @p.go:1:6
b0:
  jump b1 @6:2
b1:
  v2 = const 2 @6:2
  v3 = const 3 @6:2
  println v3 @6:2
  jump b2 @6:2
b2:
  println v2 @6:2
  ret @6:2
`,
	},
	{
		// i := 0; for i < 3 { i++ }; println(i): nothing is constant.
		name: "loop",
		build: func(_ *ir.Program, f *ir.Func) {
			i := f.NewSlot("i", ir.I64, ir.Pos{Line: 2, Col: 2})
			b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
			f.Store(b0, i, f.Const(b0, 0, at), at)
			f.Jump(b0, b1, at)
			f.Branch(b1, f.Compare(b1, ir.Lt, f.Load(b1, i, at), f.Const(b1, 3, at), at), b2, b3, at)
			f.Store(b2, i, f.Binary(b2, ir.Add, f.Load(b2, i, at), f.Const(b2, 1, at), at), at)
			f.Jump(b2, b1, at)
			f.Println(b3, []*ir.Instr{f.Load(b3, i, at)}, at)
			f.Ret(b3, at)
		},
		want: `func main ssa @p.go:1:6
b0:
  v0 = const 0 @6:2
  jump b1 @6:2
b1:
  v8 = phi i64, b0.0: v0, b2.0: v6 @2:2
  v2 = const 3 @6:2
  v3 = lt v8, v2 @6:2
  branch v3, b2, b3 @6:2
b2:
  v5 = const 1 @6:2
  v6 = add v8, v5 @6:2
  jump b1 @6:2
b3:
  println v8 @6:2
  ret @6:2
`,
	},
	{
		// The branch of b1 has the join as both targets; folding it keeps
		// the value of the edge it takes, renumbered to index 0.
		name: "parallel edges",
		build: func(_ *ir.Program, f *ir.Func) {
			x := f.NewSlot("x", ir.I64, ir.Pos{Line: 2, Col: 2})
			b0, b1, b2 := f.Entry(), f.NewBlock(), f.NewBlock()
			f.Store(b0, x, f.Const(b0, 0, at), at)
			f.Branch(b0, f.BoolConst(b0, true, at), b1, b2, at)
			f.Store(b1, x, f.Const(b1, 5, at), at)
			f.Branch(b1, f.BoolConst(b1, false, at), b2, b2, at)
			f.Println(b2, []*ir.Instr{f.Load(b2, x, at)}, at)
			f.Ret(b2, at)
		},
		want: `func main ssa @p.go:1:6
b0:
  jump b1 @6:2
b1:
  v2 = const 5 @6:2
  jump b2 @6:2
b2:
  println v2 @6:2
  ret @6:2
`,
	},
	{
		// x := 6; y := 0; if y == 0 { panic } else { println(x / y) }, as
		// lowering's check builds it: the check folds to the panic, and the
		// division by zero, now unreachable, is removed without being
		// folded.
		name: "division by a constant zero",
		build: func(_ *ir.Program, f *ir.Func) {
			x, y := f.NewSlot("x", ir.I64, ir.Pos{Line: 2, Col: 2}), f.NewSlot("y", ir.I64, ir.Pos{Line: 3, Col: 2})
			b0, panicking, ok := f.Entry(), f.NewBlock(), f.NewBlock()
			f.Store(b0, x, f.Const(b0, 6, at), at)
			f.Store(b0, y, f.Const(b0, 0, at), at)
			zero := f.Compare(b0, ir.Eq, f.Load(b0, y, at), f.Const(b0, 0, at), at)
			f.Branch(b0, zero, panicking, ok, at)
			f.Panic(panicking, ir.PanicDivide, at)
			q := f.Binary(ok, ir.Div, f.Load(ok, x, at), f.Load(ok, y, at), at)
			f.Println(ok, []*ir.Instr{q}, at)
			f.Ret(ok, at)
		},
		want: `func main ssa @p.go:1:6
b0:
  jump b1 @6:2
b1:
  panic divide @6:2
`,
	},
	{
		// Arithmetic of constants through phis whose values are all equal:
		// x := 7; if g { x = 7 }; println(int8(x*20) >> 1): 140 wraps to -116.
		name: "constant phi",
		build: func(p *ir.Program, f *ir.Func) {
			g := p.NewGlobal("g", ir.Bool, "p.go", ir.Pos{Line: 1, Col: 5})
			x := f.NewSlot("x", ir.I64, ir.Pos{Line: 2, Col: 2})
			b0, b1, b2 := f.Entry(), f.NewBlock(), f.NewBlock()
			f.Store(b0, x, f.Const(b0, 7, at), at)
			f.Branch(b0, f.GLoad(b0, g, at), b1, b2, at)
			f.Store(b1, x, f.Const(b1, 7, at), at)
			f.Jump(b1, b2, at)
			m := f.Binary(b2, ir.Mul, f.Load(b2, x, at), f.Const(b2, 20, at), at)
			n := f.Conv(b2, ir.I8, m, at)
			s := f.Shift(b2, ir.Shr, n, f.TypedConst(b2, ir.U64, 1, at), at)
			f.Println(b2, []*ir.Instr{s}, at)
			f.Ret(b2, at)
		},
		want: `global g0 g bool @p.go:1:5

func main ssa @p.go:1:6
b0:
  v1 = gload g0 @6:2
  branch v1, b1, b2 @6:2
b1:
  jump b2 @6:2
b2:
  v8 = const i8 -58 @6:2
  println v8 @6:2
  ret @6:2
`,
	},
}

func TestFold(t *testing.T) {
	for _, fx := range foldFixtures {
		t.Run(fx.name, func(t *testing.T) {
			p := &ir.Program{}
			f := p.NewFunc("main", "p.go", ir.Pos{Line: 1, Col: 6})
			fx.build(p, f)
			if err := ir.Promote(f); err != nil {
				t.Fatal(err)
			}
			if errs := ir.Verify(p); len(errs) != 0 {
				t.Fatalf("promoted fixture does not verify: %v", errs)
			}
			before := p.String()
			changed, err := Fold(f)
			if err != nil {
				t.Fatal(err)
			}
			if errs := ir.Verify(p); len(errs) != 0 {
				t.Fatalf("folded function does not verify: %v\n%s", errs, p)
			}
			if got := p.String(); got != fx.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, fx.want)
			}
			if changed != (p.String() != before) {
				t.Errorf("Fold reports changed %v", changed)
			}
			if again, err := Fold(f); again || err != nil {
				t.Errorf("a second Fold changed the function (%v, %v)", again, err)
			}
		})
	}
}

func TestFoldRejects(t *testing.T) {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", ir.Pos{Line: 1, Col: 6})
	f.Ret(f.Entry(), at)
	if _, err := Fold(f); err == nil || !strings.Contains(err.Error(), "not in SSA form") {
		t.Errorf("error %v, want one about SSA form", err)
	}
}
