package opt

import (
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// The DCE fixtures are ordinary functions main, built as lowering would build
// them, promoted, and then given to DCE alone; want is the result.
var dceFixtures = []struct {
	name  string
	build func(p *ir.Program, f *ir.Func)
	want  string
}{
	{
		// _ = g + 1; println(2): the load, the addition, and its constant go.
		name: "dead chain",
		build: func(p *ir.Program, f *ir.Func) {
			g := p.NewGlobal("g", ir.I64, "p.go", ir.Pos{Line: 1, Col: 5})
			b := f.Entry()
			f.Binary(b, ir.Add, f.GLoad(b, g, at), f.Const(b, 1, at), at)
			f.Println(b, []*ir.Instr{f.Const(b, 2, at)}, at)
			f.Ret(b, at)
		},
		want: `global g0 g i64 @p.go:1:5

func main ssa @p.go:1:6
b0:
  v3 = const 2 @6:2
  println v3 @6:2
  ret @6:2
`,
	},
	{
		// s := 0; for i := 0; i < 3; i++ { s += i }; println(i): s's phi and
		// its addition use only each other.
		name: "dead phi cycle",
		build: func(_ *ir.Program, f *ir.Func) {
			i, s := f.NewSlot("i", ir.I64, ir.Pos{Line: 2, Col: 2}), f.NewSlot("s", ir.I64, ir.Pos{Line: 3, Col: 2})
			b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
			f.Store(b0, i, f.Const(b0, 0, at), at)
			f.Store(b0, s, f.Const(b0, 0, at), at)
			f.Jump(b0, b1, at)
			f.Branch(b1, f.Compare(b1, ir.Lt, f.Load(b1, i, at), f.Const(b1, 3, at), at), b2, b3, at)
			f.Store(b2, s, f.Binary(b2, ir.Add, f.Load(b2, s, at), f.Load(b2, i, at), at), at)
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
  v12 = phi i64, b0.0: v0, b2.0: v10 @2:2
  v3 = const 3 @6:2
  v4 = lt v12, v3 @6:2
  branch v4, b2, b3 @6:2
b2:
  v9 = const 1 @6:2
  v10 = add v12, v9 @6:2
  jump b1 @6:2
b3:
  println v12 @6:2
  ret @6:2
`,
	},
	{
		// An unused call result, a global store, and lowering's check of a
		// dead division stay; the division goes.
		name: "effects stay",
		build: func(p *ir.Program, f *ir.Func) {
			g := p.NewGlobal("g", ir.I64, "p.go", ir.Pos{Line: 1, Col: 5})
			h := p.NewFunc("h", "p.go", ir.Pos{Line: 9, Col: 6})
			h.Results = []ir.Type{ir.I64}
			h.Ret(h.Entry(), at, h.Const(h.Entry(), 4, at))
			if err := ir.Promote(h); err != nil {
				panic(err)
			}
			b0, panicking, ok := f.Entry(), f.NewBlock(), f.NewBlock()
			x := f.Call(b0, h, nil, at)
			f.GStore(b0, g, x, at)
			y := f.GLoad(b0, g, at)
			f.Branch(b0, f.Compare(b0, ir.Eq, y, f.Const(b0, 0, at), at), panicking, ok, at)
			f.Panic(panicking, ir.PanicDivide, at)
			f.Binary(ok, ir.Div, f.Const(ok, 8, at), y, at)
			f.Call(ok, h, nil, at)
			f.Ret(ok, at)
		},
		want: `global g0 g i64 @p.go:1:5

func main ssa @p.go:1:6
b0:
  v0 = call h @6:2
  gstore g0, v0 @6:2
  v1 = gload g0 @6:2
  v2 = const 0 @6:2
  v3 = eq v1, v2 @6:2
  branch v3, b1, b2 @6:2
b1:
  panic divide @6:2
b2:
  v6 = call h @6:2
  ret @6:2

func h() i64 ssa @p.go:9:6
b0:
  v0 = const 4 @6:2
  ret v0 @6:2
`,
	},
}

func TestDCE(t *testing.T) {
	for _, fx := range dceFixtures {
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
			changed, err := DCE(f)
			if err != nil {
				t.Fatal(err)
			}
			if errs := ir.Verify(p); len(errs) != 0 {
				t.Fatalf("result does not verify: %v\n%s", errs, p)
			}
			if got := p.String(); got != fx.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, fx.want)
			}
			if changed != (p.String() != before) {
				t.Errorf("DCE reports changed %v", changed)
			}
			if again, err := DCE(f); again || err != nil {
				t.Errorf("a second DCE changed the function (%v, %v)", again, err)
			}
		})
	}
}

func TestDCERejects(t *testing.T) {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", ir.Pos{Line: 1, Col: 6})
	f.Ret(f.Entry(), at)
	if _, err := DCE(f); err == nil || !strings.Contains(err.Error(), "not in SSA form") {
		t.Errorf("error %v, want one about SSA form", err)
	}
}

// TestHasEffect checks every operation's classification: exactly the
// terminators, Call, Println, Print, GStore, and Param are roots.
func TestHasEffect(t *testing.T) {
	roots := map[ir.Op]bool{ir.Ret: true, ir.Jump: true, ir.Branch: true, ir.Panic: true,
		ir.Call: true, ir.Println: true, ir.Print: true, ir.GStore: true, ir.Param: true}
	for op := ir.Const; op <= ir.Phi; op++ {
		if op == ir.Load || op == ir.Store {
			continue // not in SSA form
		}
		if got := hasEffect(&ir.Instr{Op: op}); got != roots[op] {
			t.Errorf("%s: hasEffect %v, want %v", op, got, roots[op])
		}
	}
}
