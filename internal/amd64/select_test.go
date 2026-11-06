package amd64

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// branchFunc returns a function in which the condition built by cond decides
// a Branch, with its Param x. Both targets return.
func branchFunc(cond func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr) (*ir.Func, *ir.Instr) {
	p := &ir.Program{}
	f := p.NewFunc("f", "p.go", p1)
	f.Params = []ir.Type{ir.I64}
	b := f.Entry()
	x := f.Param(b, 0, p1)
	c := cond(f, b, x)
	then, els := f.NewBlock(), f.NewBlock()
	br := f.Branch(b, c, then, els, p1)
	f.Ret(then, p1)
	f.Ret(els, p1)
	return f, br
}

func TestSelectFunc(t *testing.T) {
	lt := func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr {
		return f.Compare(b, ir.Lt, x, f.Const(b, 1, p1), p1)
	}
	cases := []struct {
		name string
		cond func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr
		neg  bool
		// fused is how many values are fused; test is the Op tested.
		fused int
		test  ir.Op
	}{
		{"compare", lt, false, 1, ir.Lt},
		{"not", func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr { return f.Not(b, lt(f, b, x), p1) }, true, 2, ir.Lt},
		{"not not", func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr { return f.Not(b, f.Not(b, lt(f, b, x), p1), p1) }, false, 3, ir.Lt},
		{"immediate between", func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr {
			c := lt(f, b, x)
			f.Const(b, 7, p1)
			return c
		}, false, 1, ir.Lt},
		{"large constant between", func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr {
			c := lt(f, b, x)
			f.Const(b, math.MaxInt64, p1)
			return c
		}, false, 0, ir.Lt},
		{"code between", func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr {
			c := lt(f, b, x)
			f.Neg(b, x, p1)
			return c
		}, false, 0, ir.Lt},
		{"two uses", func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr {
			c := lt(f, b, x)
			f.Println(b, []*ir.Instr{c}, p1)
			return c
		}, false, 0, ir.Lt},
		{"not of two uses", func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr {
			c := lt(f, b, x)
			f.Println(b, []*ir.Instr{c}, p1)
			return f.Not(b, c, p1)
		}, true, 1, ir.Lt},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, br := branchFunc(c.cond)
			s := selectFunc(f)
			if len(s.skip) != c.fused {
				t.Errorf("%d fused, want %d", len(s.skip), c.fused)
			}
			if got := s.cond[br]; got.neg != c.neg || got.v.Op != c.test {
				t.Errorf("tests %s, negated %v; want %s, %v", got.v.Op, got.neg, c.test, c.neg)
			}
			if err := checkSelection(f, s, nil); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestSelectImmediates(t *testing.T) {
	p := &ir.Program{}
	f := p.NewFunc("f", "p.go", p1)
	b := f.Entry()
	konst := func(ty ir.Type, v int64) *ir.Instr { return f.TypedConst(b, ty, v, p1) }
	cases := []struct {
		v    *ir.Instr
		want int64
		imm  bool
	}{
		{konst(ir.I64, 0), 0, true},
		{konst(ir.I64, math.MaxInt32), math.MaxInt32, true},
		{konst(ir.I64, math.MaxInt32+1), 0, false},
		{konst(ir.I64, math.MinInt32), math.MinInt32, true},
		{konst(ir.I64, math.MinInt32-1), 0, false},
		{konst(ir.U32, math.MaxUint32), 0, false},
		// -1 as u64 is all ones, which the immediate -1 sign-extends to.
		{f.Conv(b, ir.U64, konst(ir.I64, -1), p1), -1, true},
		// -1 as u32 is 2^32-1, which no immediate gives.
		{f.Conv(b, ir.U32, konst(ir.I64, -1), p1), 0, false},
		{f.Conv(b, ir.U8, konst(ir.I64, 300), p1), 44, true},
		{f.Conv(b, ir.I8, konst(ir.I64, 200), p1), -56, true},
		{f.Conv(b, ir.U64, f.Conv(b, ir.U8, konst(ir.I64, 257), p1), p1), 1, true},
		// The operand is not an immediate, even if the result would fit.
		{f.Conv(b, ir.U8, konst(ir.I64, 1<<40), p1), 0, false},
	}
	f.Ret(b, p1)
	s := selectFunc(f)
	for i, c := range cases {
		got, ok := s.imm[c.v]
		if ok != c.imm || got != c.want {
			t.Errorf("case %d: %s: immediate %d, %v; want %d, %v", i, c.v, got, ok, c.want, c.imm)
		}
	}
	if err := checkSelection(f, s, nil); err != nil {
		t.Error(err)
	}
}

// TestCheckSelectionRejects plants each kind of bad selection.
func TestCheckSelectionRejects(t *testing.T) {
	type sel = selection
	var f *ir.Func
	var br *ir.Instr
	build := func(cond func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr) *sel {
		f, br = branchFunc(cond)
		return selectFunc(f)
	}
	plainLt := func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr {
		return f.Compare(b, ir.Lt, x, f.Const(b, 1, p1), p1)
	}
	cases := []struct {
		name, want string
		plant      func() (*sel, map[*ir.Instr]int)
	}{
		{"too large", "does not fit", func() (*sel, map[*ir.Instr]int) {
			s := build(plainLt)
			s.imm[f.Entry().Instrs[1]] = math.MaxInt32 + 1
			return s, nil
		}},
		{"wrong value", "not the immediate", func() (*sel, map[*ir.Instr]int) {
			s := build(plainLt)
			s.imm[f.Entry().Instrs[1]] = 2
			return s, nil
		}},
		{"not a constant", "not the immediate", func() (*sel, map[*ir.Instr]int) {
			s := build(plainLt)
			s.imm[f.Entry().Instrs[0]] = 2
			return s, nil
		}},
		{"immediate with a register", "a register", func() (*sel, map[*ir.Instr]int) {
			s := build(plainLt)
			return s, map[*ir.Instr]int{f.Entry().Instrs[1]: 0}
		}},
		{"fused with a register", "a register", func() (*sel, map[*ir.Instr]int) {
			s := build(plainLt)
			return s, map[*ir.Instr]int{br.Args[0]: 0}
		}},
		{"code between", "code between", func() (*sel, map[*ir.Instr]int) {
			s := build(func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr {
				c := plainLt(f, b, x)
				f.Neg(b, x, p1)
				return c
			})
			s.skip[br.Args[0]] = true
			s.cond[br] = condition{v: br.Args[0]}
			return s, nil
		}},
		{"two uses", "2 uses", func() (*sel, map[*ir.Instr]int) {
			s := build(func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr {
				return plainLt(f, b, x)
			})
			// A second use, in the first target.
			f.Blocks[1].Instrs = append([]*ir.Instr{{Op: ir.Println, Args: []*ir.Instr{br.Args[0]}, ID: -1}}, f.Blocks[1].Instrs...)
			return s, nil
		}},
		{"fused value not a condition", "cannot be fused", func() (*sel, map[*ir.Instr]int) {
			s := build(func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr {
				return f.Conv(b, ir.Bool, x, p1)
			})
			s.skip[br.Args[0]] = true
			return s, nil
		}},
		{"wrong negation", "wrong condition", func() (*sel, map[*ir.Instr]int) {
			s := build(func(f *ir.Func, b *ir.Block, x *ir.Instr) *ir.Instr { return f.Not(b, plainLt(f, b, x), p1) })
			c := s.cond[br]
			c.neg = !c.neg
			s.cond[br] = c
			return s, nil
		}},
		{"fused into nothing", "fused into branches", func() (*sel, map[*ir.Instr]int) {
			s := build(plainLt)
			s.skip[f.Entry().Instrs[0]] = true
			return s, nil
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, regs := c.plant()
			err := checkSelection(f, s, regs)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %v, want one containing %q", err, c.want)
			}
		})
	}
}

// TestSelectConditionTables checks the condition tables against the meaning
// of the comparisons, on values at every ordering, signed and unsigned.
func TestSelectConditionTables(t *testing.T) {
	holds := func(cc string, x, y int64) bool {
		ux, uy := uint64(x), uint64(y)
		return map[string]bool{"e": x == y, "ne": x != y, "l": x < y, "le": x <= y, "g": x > y, "ge": x >= y,
			"b": ux < uy, "be": ux <= uy, "a": ux > uy, "ae": ux >= uy}[cc]
	}
	if len(inverse) != 10 {
		t.Fatalf("inverse has %d codes", len(inverse))
	}
	for _, x := range []int64{math.MinInt64, -1, 0, 1, math.MaxInt64} {
		for _, y := range []int64{math.MinInt64, -1, 0, 1, math.MaxInt64} {
			for cc, inv := range inverse {
				if holds(cc, x, y) == holds(inv, x, y) {
					t.Errorf("%s and its inverse %s agree on %d, %d", cc, inv, x, y)
				}
			}
			for op, sw := range swapped {
				for _, table := range []map[ir.Op]string{setcc, setccUnsigned} {
					a, b := strings.TrimPrefix(table[op], "set"), strings.TrimPrefix(table[sw], "set")
					if holds(a, x, y) != holds(b, y, x) {
						t.Errorf("%s of %d, %d differs from the swapped %s", a, x, y, b)
					}
				}
			}
		}
	}
}

// TestEmitWithoutSelection pins what Select must leave alone: without it, no
// form of selection appears, so -O0 and the RA-0 variant are what they were.
func TestEmitWithoutSelection(t *testing.T) {
	for _, opts := range []EmitOptions{{}, {Registers: true}} {
		asm, err := EmitWith(sample(), opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, form := range []string{"leaq", "testq\t%rbx", "movl\t$", "jne\t.L", "jge\t.L", "cmpq\t$0"} {
			if bytes.Contains(asm, []byte(form)) {
				t.Errorf("%+v: output contains %q", opts, form)
			}
		}
	}
}

// TestForwarders checks the blocks that are not emitted: a chain of empty
// blocks goes straight to its end, one with phi copies stays, and a cycle of
// empty blocks keeps one block, which still jumps to itself.
func TestForwarders(t *testing.T) {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	b0, b1, b2, b3, b4, b5 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	x := f.GLoad(b0, p.NewGlobal("g", ir.Bool, "p.go", p1), p1)
	f.Branch(b0, x, b4, b1, p1)
	f.Jump(b1, b2, p1) // b1 -> b2 -> b3
	f.Jump(b2, b3, p1)
	f.Ret(b3, p1)
	f.Jump(b4, b5, p1) // b4 <-> b5
	f.Jump(b5, b4, p1)
	asm, err := EmitWith(p, EmitOptions{Select: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(asm)
	for _, gone := range []string{".L0.b1:", ".L0.b2:", ".L0.b5:"} {
		if strings.Contains(s, gone) {
			t.Errorf("%s is emitted:\n%s", gone, s)
		}
	}
	for _, kept := range []string{".L0.b3:", ".L0.b4:", "jmp\t.L0.b4"} {
		if !strings.Contains(s, kept) {
			t.Errorf("%s is not emitted:\n%s", kept, s)
		}
	}
	if out, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 0 || out != "" {
		t.Errorf("exit %d, output %q", code, out)
	}
}
