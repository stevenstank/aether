package amd64

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

var intTypes = []ir.Type{ir.I64, ir.I8, ir.I16, ir.I32, ir.U8, ir.U16, ir.U32, ir.U64}

// TestPreserves checks preserves against its meaning: it holds exactly when
// conversion leaves every canonical value of the source type unchanged.
func TestPreserves(t *testing.T) {
	samples := []int64{0, 1, -1, 127, 128, -128, -129, 255, 256, 32767, 32768, -32768, 65535, 65536,
		1<<31 - 1, 1 << 31, -1 << 31, 1<<32 - 1, 1 << 32, 1<<63 - 1, -1 << 63}
	for _, from := range intTypes {
		for _, to := range intTypes {
			same := true
			for _, s := range samples {
				if v := from.Canonical(s); to.Canonical(v) != v {
					same = false
				}
			}
			if got := preserves(from, to); got != same {
				t.Errorf("preserves(%s, %s) = %v, want %v", from, to, got, same)
			}
		}
	}
}

// loc is where an operand or result lives in arithCase: a register of
// allocRegs, a cell, or an immediate.
type loc struct {
	reg, cell int // -1 if not
	imm       bool
}

var (
	rBX, rSI, rDI = loc{0, -1, false}, loc{1, -1, false}, loc{2, -1, false}
	cell0, cell1  = loc{-1, 0, false}, loc{-1, 1, false}
	immLoc        = loc{-1, -1, true}
)

// arithEmitter returns an emitter for a function computing op on operands of
// type xt (the result of type rt) with the given locations, and the
// instruction. If same, the operation's two operands are one value, at xl.
func arithEmitter(op ir.Op, xt, rt ir.Type, xl, yl, rl loc, same bool) (*emitter, *ir.Instr) {
	p := &ir.Program{}
	f := p.NewFunc("f", "p.go", p1)
	f.Params = []ir.Type{xt, xt}
	b := f.Entry()
	var x *ir.Instr
	if xl.imm {
		x = f.TypedConst(b, xt, xt.Canonical(-100), p1)
	} else {
		x = f.Param(b, 0, p1)
	}
	var y *ir.Instr
	if yl.imm {
		y = f.TypedConst(b, xt, xt.Canonical(77), p1)
	} else {
		y = f.Param(b, 1, p1)
	}
	if same {
		y = x
	}
	var in *ir.Instr
	switch op {
	case ir.Neg:
		in = f.Neg(b, x, p1)
	case ir.Com:
		in = f.Com(b, x, p1)
	case ir.Conv:
		in = f.Conv(b, rt, x, p1)
	default:
		in = f.Binary(b, op, x, y, p1)
	}
	f.Ret(b, p1)
	sel := &selection{imm: map[*ir.Instr]int64{}, skip: valueSet{}, cond: map[*ir.Instr]condition{}, twoAddr: true}
	regs, cells := map[*ir.Instr]int{}, map[*ir.Instr]int{}
	place := func(v *ir.Instr, l loc) {
		switch {
		case l.imm:
			sel.imm[v] = v.Const
		case l.reg >= 0:
			regs[v] = l.reg
		default:
			cells[v] = l.cell
		}
	}
	place(x, xl)
	if !same {
		place(y, yl)
	}
	place(in, rl)
	fr, err := NewFrame(0, 2, 0)
	if err != nil {
		panic(err)
	}
	return &emitter{b: &bytes.Buffer{}, fr: fr, f: f, regs: regs, cells: cells, sel: sel}, in
}

// TestTwoAddressForms emits every register form for every operation, type, and
// arrangement of operand and result registers, and checks each with
// checkArith.
func TestTwoAddressForms(t *testing.T) {
	type layout struct {
		name       string
		xl, yl, rl loc
		same       bool
	}
	binary := []layout{
		{"distinct", rBX, rSI, rDI, false},
		{"into x", rBX, rSI, rBX, false},
		{"into y", rBX, rSI, rSI, false},
		{"x op x", rBX, rBX, rDI, true},
		{"x op x into x", rBX, rBX, rBX, true},
		{"x in a cell", cell0, rSI, rDI, false},
		{"x in a cell, into y", cell0, rSI, rSI, false},
		{"y in a cell", rBX, cell1, rDI, false},
		{"y in a cell, into x", rBX, cell1, rBX, false},
		{"both in cells", cell0, cell1, rDI, false},
		{"x immediate", immLoc, rSI, rDI, false},
		{"x immediate, into y", immLoc, rSI, rSI, false},
		{"y immediate", rBX, immLoc, rDI, false},
		{"y immediate, into x", rBX, immLoc, rBX, false},
	}
	unary := []layout{
		{"distinct", rBX, loc{}, rDI, false},
		{"in place", rBX, loc{}, rBX, false},
		{"from a cell", cell0, loc{}, rDI, false},
		{"immediate", immLoc, loc{}, rDI, false},
	}
	run := func(t *testing.T, op ir.Op, xt, rt ir.Type, l layout) {
		e, in := arithEmitter(op, xt, rt, l.xl, l.yl, l.rl, l.same)
		if !e.twoAddress(in) {
			t.Fatalf("%s %s→%s %s: not emitted", op, xt, rt, l.name)
		}
		if e.err != nil {
			t.Fatalf("%s %s→%s %s: %v", op, xt, rt, l.name, e.err)
		}
		if err := e.checkArith(); err != nil {
			t.Errorf("%s %s→%s %s: %v\n%s", op, xt, rt, l.name, err, e.b)
		}
		if strings.Contains(e.b.String(), "%rax") {
			t.Errorf("%s %s→%s %s: uses %%rax:\n%s", op, xt, rt, l.name, e.b)
		}
	}
	for _, op := range []ir.Op{ir.Add, ir.Sub, ir.Mul, ir.And, ir.Or, ir.Xor, ir.AndNot} {
		for _, ty := range intTypes {
			for _, l := range binary {
				run(t, op, ty, ty, l)
			}
		}
	}
	for _, op := range []ir.Op{ir.Neg, ir.Com} {
		for _, ty := range intTypes {
			for _, l := range unary {
				run(t, op, ty, ty, l)
			}
		}
	}
	for _, from := range intTypes {
		for _, to := range intTypes {
			for _, l := range unary {
				run(t, ir.Conv, from, to, l)
			}
		}
	}
}

// TestTwoAddressDeclines checks that a result in a cell, or the option off,
// leaves the accumulator sequence of instr.
func TestTwoAddressDeclines(t *testing.T) {
	e, in := arithEmitter(ir.Add, ir.I64, ir.I64, rBX, rSI, cell0, false)
	if e.twoAddress(in) || e.b.Len() > 0 {
		t.Errorf("emitted a result in a cell:\n%s", e.b)
	}
	e, in = arithEmitter(ir.Add, ir.I64, ir.I64, rBX, rSI, rDI, false)
	e.sel.twoAddr = false
	if e.twoAddress(in) || e.b.Len() > 0 {
		t.Errorf("emitted with the option off:\n%s", e.b)
	}
}

// TestTwoAddressShapes checks the instructions chosen for the arrangements
// that differ from the plain move and operation.
func TestTwoAddressShapes(t *testing.T) {
	cases := []struct {
		op         ir.Op
		xt, rt     ir.Type
		xl, yl, rl loc
		want       string
	}{
		{ir.Add, ir.I64, ir.I64, rBX, rSI, rDI, "leaq\t(%rbx,%rsi), %rdi\n"},
		{ir.Add, ir.I64, ir.I64, rBX, rSI, rBX, "addq\t%rsi, %rbx\n"},
		{ir.Add, ir.I64, ir.I64, rBX, rSI, rSI, "addq\t%rbx, %rsi\n"},
		{ir.Sub, ir.I64, ir.I64, rBX, rSI, rSI, "negq\t%rsi\n\taddq\t%rbx, %rsi\n"},
		{ir.Sub, ir.I64, ir.I64, rBX, rSI, rDI, "movq\t%rbx, %rdi\n\tsubq\t%rsi, %rdi\n"},
		{ir.AndNot, ir.I64, ir.I64, rBX, rSI, rSI, "notq\t%rsi\n\tandq\t%rbx, %rsi\n"},
		{ir.AndNot, ir.I64, ir.I64, rBX, rSI, rBX, "movq\t%rsi, %rcx\n\tnotq\t%rcx\n\tandq\t%rcx, %rbx\n"},
		{ir.Xor, ir.U32, ir.U32, rBX, rSI, rBX, "xorq\t%rsi, %rbx\n"},
		{ir.Add, ir.U32, ir.U32, rBX, rSI, rBX, "addq\t%rsi, %rbx\n\tmovl\t%ebx, %ebx\n"},
		{ir.Conv, ir.U8, ir.U32, rBX, loc{}, rDI, "movq\t%rbx, %rdi\n"},
		{ir.Conv, ir.I8, ir.U32, rBX, loc{}, rDI, "movl\t%ebx, %edi\n"},
		{ir.Conv, ir.I64, ir.I8, rBX, loc{}, rBX, "movsbq\t%bl, %rbx\n"},
		{ir.Conv, ir.U16, ir.I64, rBX, loc{}, rBX, ""},
		{ir.Com, ir.I8, ir.I8, rBX, loc{}, rBX, "notq\t%rbx\n"},
		{ir.Com, ir.U8, ir.U8, rBX, loc{}, rBX, "notq\t%rbx\n\tmovzbl\t%bl, %ebx\n"},
	}
	for _, c := range cases {
		e, in := arithEmitter(c.op, c.xt, c.rt, c.xl, c.yl, c.rl, false)
		e.twoAddress(in)
		want := c.want
		if want != "" {
			want = "\t" + want
		}
		if got := e.b.String(); got != want {
			t.Errorf("%s: got\n%s\nwant\n%s", in, got, want)
		}
	}
}

// TestCheckArithRejects plants wrong sequences, which checkArith must reject.
func TestCheckArithRejects(t *testing.T) {
	cases := []struct {
		name   string
		op     ir.Op
		ty     ir.Type
		rl     loc
		code   string
		reason string
	}{
		{"operand overwritten", ir.Sub, ir.I64, rSI, "movq %rbx, %rsi; subq %rsi, %rsi", "computes"},
		{"commuted subtraction", ir.Sub, ir.I64, rSI, "subq %rbx, %rsi", "computes"},
		{"missing canonical form", ir.Add, ir.I8, rDI, "leaq (%rbx,%rsi), %rdi", "computes"},
		{"wrong extension", ir.Add, ir.U8, rDI, "leaq (%rbx,%rsi), %rdi; movsbq %dil, %rdi", "computes"},
		{"wrong operation", ir.Or, ir.I64, rDI, "movq %rbx, %rdi; xorq %rsi, %rdi", "computes"},
		{"another register written", ir.Add, ir.I64, rDI, "movq %rbx, %r8; addq %rsi, %r8; movq %r8, %rdi", "writes %r8"},
		{"%rax written", ir.Add, ir.I64, rDI, "movq %rbx, %rax; addq %rsi, %rax; movq %rax, %rdi", "writes %rax"},
		{"operand register written", ir.Add, ir.I64, rDI, "addq %rsi, %rbx; movq %rbx, %rdi", "writes %rbx"},
		{"result never written", ir.Add, ir.I64, rDI, "addq %rsi, %rdi", "holds no operand"},
		{"unknown instruction", ir.Add, ir.I64, rDI, "movq %rbx, %rdi; shlq $1, %rdi", "unexpected"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, in := arithEmitter(c.op, c.ty, c.ty, rBX, rSI, c.rl, false)
			for _, l := range strings.Split(c.code, "; ") {
				op, rest, _ := strings.Cut(l, " ")
				fmt.Fprintf(e.b, "\t%s\t%s\n", op, rest)
			}
			e.arith = []arithSite{{in, 0, e.b.Len()}}
			err := e.checkArith()
			if err == nil || !strings.Contains(err.Error(), c.reason) {
				t.Errorf("error %v, want one containing %q", err, c.reason)
			}
		})
	}
}

// TestCheckArithNeedsEveryCanonicalForm deletes the canonicalization that ends
// each form needing one, at every narrow type, and checks that checkArith
// rejects what is left: its vectors reach every width's overflow.
func TestCheckArithNeedsEveryCanonicalForm(t *testing.T) {
	type form struct {
		op     ir.Op
		xt, rt ir.Type
	}
	var forms []form
	for _, ty := range intTypes {
		if ty.Width() == 64 {
			continue
		}
		forms = append(forms, form{ir.Add, ty, ty}, form{ir.Sub, ty, ty}, form{ir.Mul, ty, ty}, form{ir.Neg, ty, ty})
		if !ty.Signed() {
			forms = append(forms, form{ir.Com, ty, ty})
		}
	}
	for _, from := range intTypes {
		for _, to := range intTypes {
			if !preserves(from, to) {
				forms = append(forms, form{ir.Conv, from, to})
			}
		}
	}
	for _, f := range forms {
		e, in := arithEmitter(f.op, f.xt, f.rt, rBX, rSI, rBX, false)
		if !e.twoAddress(in) {
			t.Fatalf("%s: not emitted", in)
		}
		code := strings.TrimSuffix(e.b.String(), "\n")
		last := strings.LastIndex(code, "\n")
		if !strings.HasPrefix(code[last+1:], "\tmov") {
			t.Fatalf("%s: does not end in its canonical form:\n%s", in, code)
		}
		e.b.Truncate(last + 1)
		e.arith = []arithSite{{in, 0, e.b.Len()}}
		if err := e.checkArith(); err == nil {
			t.Errorf("%s %s→%s: accepted without its canonical form:\n%s", in, f.xt, f.rt, e.b)
		}
	}
}
