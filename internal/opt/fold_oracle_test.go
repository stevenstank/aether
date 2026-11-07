package opt

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

var foldTypes = []ir.Type{ir.I64, ir.I8, ir.I16, ir.I32, ir.U8, ir.U16, ir.U32, ir.U64}

// oracleValues are the constants of type t the oracle uses: around the edges
// of every width, reduced to t.
func oracleValues(t ir.Type) []int64 {
	var vs []int64
	seen := map[int64]bool{}
	for _, v := range []int64{0, 1, 2, 3, 7, -1, -2, -7, 63, 64, 65,
		127, 128, -128, -129, 255, 256, 32767, 32768, -32768, 65535,
		1<<31 - 1, 1 << 31, -1 << 31, 1<<32 - 1, 1<<63 - 1, -1 << 63} {
		if c := t.Canonical(v); !seen[c] {
			seen[c] = true
			vs = append(vs, c)
		}
	}
	return vs
}

// TestFoldValueOracle checks FoldValue against the interpreter, an
// independent implementation of the IR's semantics: for every operation it
// folds, on every type and pair of oracleValues, a program printing the
// operation's result prints the same before and after Fold, and after Fold
// it prints only constants.
func TestFoldValueOracle(t *testing.T) {
	at := ir.Pos{Line: 1, Col: 1}
	binary := []ir.Op{ir.Add, ir.Sub, ir.Mul, ir.And, ir.Or, ir.Xor, ir.AndNot, ir.Div, ir.Rem,
		ir.Shl, ir.Shr, ir.Eq, ir.Ne, ir.Lt, ir.Le, ir.Gt, ir.Ge}
	type gen func(f *ir.Func, b *ir.Block)
	run := func(name string, body gen) {
		p := &ir.Program{}
		f := p.NewFunc("main", "p.go", at)
		body(f, f.Entry())
		f.Ret(f.Entry(), at)
		if err := ir.Promote(f); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var want, got bytes.Buffer
		if _, err := interp.Run(p, &want); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := Fold(f); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if errs := ir.Verify(p); len(errs) > 0 {
			t.Fatalf("%s: folded program does not verify: %v", name, errs)
		}
		for _, in := range f.Entry().Instrs {
			if in.Op == ir.Println && in.Args[0].Op != ir.Const {
				t.Errorf("%s: not folded: %s", name, in.Args[0])
			}
		}
		if _, err := interp.Run(p, &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.String() != want.String() {
			t.Errorf("%s: folded results differ from the interpreter's", name)
		}
	}
	for _, ty := range foldTypes {
		vals := oracleValues(ty)
		for _, op := range binary {
			run(fmt.Sprintf("%s %s", op, ty), func(f *ir.Func, b *ir.Block) {
				for _, x := range vals {
					for _, y := range vals {
						yt := ty
						if op == ir.Shl || op == ir.Shr {
							yt, y = ir.U64, ir.U64.Canonical(y)
						}
						if (op == ir.Div || op == ir.Rem) && y == 0 {
							continue
						}
						xc, yc := f.TypedConst(b, ty, x, at), f.TypedConst(b, yt, y, at)
						var r *ir.Instr
						switch {
						case op.IsComparison():
							r = f.Compare(b, op, xc, yc, at)
						case op == ir.Shl || op == ir.Shr:
							r = f.Shift(b, op, xc, yc, at)
						default:
							r = f.Binary(b, op, xc, yc, at)
						}
						f.Println(b, []*ir.Instr{r}, at)
					}
				}
			})
		}
		run(fmt.Sprintf("unary %s", ty), func(f *ir.Func, b *ir.Block) {
			for _, x := range vals {
				f.Println(b, []*ir.Instr{f.Neg(b, f.TypedConst(b, ty, x, at), at)}, at)
				f.Println(b, []*ir.Instr{f.Com(b, f.TypedConst(b, ty, x, at), at)}, at)
				for _, to := range foldTypes {
					f.Println(b, []*ir.Instr{f.Conv(b, to, f.TypedConst(b, ty, x, at), at)}, at)
				}
			}
		})
	}
	run("bool", func(f *ir.Func, b *ir.Block) {
		for _, x := range []bool{false, true} {
			f.Println(b, []*ir.Instr{f.Not(b, f.BoolConst(b, x, at), at)}, at)
			for _, y := range []bool{false, true} {
				for _, op := range []ir.Op{ir.Eq, ir.Ne} {
					f.Println(b, []*ir.Instr{f.Compare(b, op, f.BoolConst(b, x, at), f.BoolConst(b, y, at), at)}, at)
				}
			}
		}
	})
}

// TestFoldValueDeclines checks that FoldValue folds no division by zero and
// no instruction with an effect or a non-constant operand.
func TestFoldValueDeclines(t *testing.T) {
	at := ir.Pos{Line: 1, Col: 1}
	p := &ir.Program{}
	g := p.NewGlobal("g", ir.I64, "p.go", at)
	f := p.NewFunc("main", "p.go", at)
	b := f.Entry()
	one, zero := f.Const(b, 1, at), f.Const(b, 0, at)
	cases := []*ir.Instr{
		f.Binary(b, ir.Div, one, zero, at),
		f.Binary(b, ir.Rem, one, zero, at),
		f.Binary(b, ir.Add, one, f.GLoad(b, g, at), at),
		f.GStore(b, g, one, at),
		f.Println(b, []*ir.Instr{one}, at),
		f.Const(b, 5, at),
	}
	for _, in := range cases {
		if v, ok := FoldValue(in); ok {
			t.Errorf("%s folds to %d", in, v)
		}
	}
}
