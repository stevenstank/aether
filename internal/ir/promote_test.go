package ir

import (
	"strings"
	"testing"
)

// The promotion fixtures are ordinary functions main, built as lowering
// would build them, with the SSA form Promote must give.
var promoteFixtures = []struct {
	name  string
	build func(f *Func)
	want  string
}{
	{
		// x := 1; if c { x = 2; y := 3; println(y) }; println(x). x needs
		// a phi at the join; y is not live there, so pruning gives it none.
		name: "diamond with pruning",
		build: func(f *Func) {
			x, y := f.NewSlot("x", I64, Pos{2, 2}), f.NewSlot("y", I64, Pos{3, 2})
			b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
			f.Store(b0, x, f.Const(b0, 1, at), at)
			f.Branch(b0, f.BoolConst(b0, true, at), b1, b2, at)
			f.Store(b1, x, f.Const(b1, 2, at), at)
			f.Store(b1, y, f.Const(b1, 3, at), at)
			f.Println(b1, []*Instr{f.Load(b1, y, at)}, at)
			f.Jump(b1, b3, at)
			f.Jump(b2, b3, at)
			f.Println(b3, []*Instr{f.Load(b3, x, at)}, at)
			f.Ret(b3, at)
		},
		want: `func main ssa @p.go:1:6
b0:
  v0 = const 1 @6:2
  v1 = const true @6:2
  branch v1, b1, b2 @6:2
b1:
  v2 = const 2 @6:2
  v3 = const 3 @6:2
  println v3 @6:2
  jump b3 @6:2
b2:
  jump b3 @6:2
b3:
  v6 = phi i64, b1.0: v2, b2.0: v0 @2:2
  println v6 @6:2
  ret @6:2
`,
	},
	{
		// i := 0; for i < 3 { i++ }; println(i)
		name: "loop",
		build: func(f *Func) {
			i := f.NewSlot("i", I64, Pos{2, 2})
			b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
			f.Store(b0, i, f.Const(b0, 0, at), at)
			f.Jump(b0, b1, at)
			f.Branch(b1, f.Compare(b1, Lt, f.Load(b1, i, at), f.Const(b1, 3, at), at), b2, b3, at)
			f.Store(b2, i, f.Binary(b2, Add, f.Load(b2, i, at), f.Const(b2, 1, at), at), at)
			f.Jump(b2, b1, at)
			f.Println(b3, []*Instr{f.Load(b3, i, at)}, at)
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
		// x := 0; if c { x = 5; branch to the join along both edges };
		// println(x). The phi has a value for each parallel edge.
		name: "parallel edges",
		build: func(f *Func) {
			x := f.NewSlot("x", I64, Pos{2, 2})
			b0, b1, b2 := f.Entry(), f.NewBlock(), f.NewBlock()
			f.Store(b0, x, f.Const(b0, 0, at), at)
			f.Branch(b0, f.BoolConst(b0, true, at), b1, b2, at)
			f.Store(b1, x, f.Const(b1, 5, at), at)
			f.Branch(b1, f.BoolConst(b1, false, at), b2, b2, at)
			f.Println(b2, []*Instr{f.Load(b2, x, at)}, at)
			f.Ret(b2, at)
		},
		want: `func main ssa @p.go:1:6
b0:
  v0 = const 0 @6:2
  v1 = const true @6:2
  branch v1, b1, b2 @6:2
b1:
  v2 = const 5 @6:2
  v3 = const false @6:2
  branch v3, b2, b2 @6:2
b2:
  v5 = phi i64, b0.1: v0, b1.0: v2, b1.1: v2 @2:2
  println v5 @6:2
  ret @6:2
`,
	},
}

func TestPromote(t *testing.T) {
	for _, fx := range promoteFixtures {
		t.Run(fx.name, func(t *testing.T) {
			p := &Program{}
			f := p.NewFunc("main", "p.go", Pos{1, 6})
			fx.build(f)
			if errs := Verify(p); len(errs) != 0 {
				t.Fatalf("fixture does not verify: %v", errs)
			}
			if err := Promote(f); err != nil {
				t.Fatal(err)
			}
			if errs := Verify(p); len(errs) != 0 {
				t.Errorf("promoted function does not verify: %v\n%s", errs, p)
			}
			if got := p.String(); got != fx.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, fx.want)
			}
		})
	}
}

// TestPromoteRejects checks that Promote refuses what it cannot convert,
// leaving the function unchanged.
func TestPromoteRejects(t *testing.T) {
	tests := []struct {
		name  string
		build func(f *Func)
		want  string
	}{
		{"already SSA", func(f *Func) {
			f.SSA = true
			f.Ret(f.Entry(), at)
		}, "already in SSA form"},
		{"edge into the entry", func(f *Func) {
			x := f.NewSlot("x", I64, at)
			b0, b1 := f.Entry(), f.NewBlock()
			f.Store(b0, x, f.Const(b0, 1, at), at)
			f.Branch(b0, f.BoolConst(b0, false, at), b1, f.NewBlock(), at)
			f.Jump(b1, b0, at)
			f.Ret(f.Blocks[2], at)
		}, "an edge enters the entry block"},
		{"load before any store", func(f *Func) { // excluded by Verify
			x := f.NewSlot("x", I64, at)
			f.Println(f.Entry(), []*Instr{f.Load(f.Entry(), x, at)}, at)
			f.Ret(f.Entry(), at)
		}, "load of x with no reaching store"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Program{}
			f := p.NewFunc("main", "p.go", Pos{1, 6})
			tt.build(f)
			before := p.String()
			err := Promote(f)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %v, want one containing %q", err, tt.want)
			}
			if p.String() != before {
				t.Errorf("function changed:\n%s\nwas:\n%s", p, before)
			}
		})
	}
}
