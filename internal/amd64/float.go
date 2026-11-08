package amd64

// Floating point (D-032). A float is a word like any other value, so slots,
// cells, registers, phis, and both calling conventions hold it unchanged. Each
// float instruction moves its operands into %xmm0 and %xmm1, computes there
// with SSE2, and moves the result back. Values never live in vector
// registers, so the allocator, ABI-S0, and ABI-R1 need nothing new: the
// vector registers are caller-saved scratch under both (D-011, D-030). The
// sequences use only the reserved scratch %rax, %rcx, and %rdx besides,
// read every operand before writing the result, and write the result last.

import (
	"fmt"

	"github.com/stevenstank/aether/internal/ir"
)

// sse gives the scalar SSE2 suffix of a float type.
func sse(t ir.Type) string {
	if t == ir.F32 {
		return "ss"
	}
	return "sd"
}

// toXMM moves the float v into the vector register x. An F32's upper bits
// are zero, which the scalar single-precision instructions ignore.
func (e *emitter) toXMM(v *ir.Instr, x string) {
	src := e.val(v)
	if isImm(src) {
		e.move(src, "%rax")
		src = "%rax"
	}
	e.op("movq\t%s, %s", src, x)
}

// fromXMM writes the float in x to in's location, in canonical form: an
// F32 moves through %eax, which zeroes the upper 32 bits.
func (e *emitter) fromXMM(in *ir.Instr, x string) {
	if in.Type == ir.F32 {
		e.op("movd\t%s, %%eax", x)
		e.op("movq\t%%rax, %s", e.val(in))
		return
	}
	e.op("movq\t%s, %s", x, e.val(in))
}

// floatCC maps a float comparison to the set instruction that tests it after
// "ucomis x1, x0", for x0 and x1 in that order, or with the operands swapped
// if swap: ucomis sets CF for less and ZF for equal, and all of ZF, PF, and
// CF for unordered, so above and above-or-equal are false for NaN.
var floatCC = map[ir.Op]struct {
	cc   string
	swap bool
}{
	ir.FGt: {"seta", false}, ir.FGe: {"setae", false}, ir.FLt: {"seta", true}, ir.FLe: {"setae", true},
}

// float emits a float instruction.
func (e *emitter) float(in *ir.Instr) error {
	op := e.op
	switch in.Op {
	case ir.FAdd, ir.FSub, ir.FMul, ir.FDiv:
		m := map[ir.Op]string{ir.FAdd: "add", ir.FSub: "sub", ir.FMul: "mul", ir.FDiv: "div"}[in.Op]
		e.toXMM(in.Args[0], "%xmm0")
		e.toXMM(in.Args[1], "%xmm1")
		op("%s%s\t%%xmm1, %%xmm0", m, sse(in.Type))
		e.fromXMM(in, "%xmm0")
	case ir.FNeg:
		e.load(in.Args[0], "%rax")
		if in.Type == ir.F32 {
			op("btcq\t$31, %%rax")
		} else {
			op("btcq\t$63, %%rax")
		}
		op("movq\t%%rax, %s", e.val(in))
	case ir.FEq, ir.FNe, ir.FLt, ir.FLe, ir.FGt, ir.FGe:
		x, y := in.Args[0], in.Args[1]
		c := floatCC[in.Op]
		if c.swap {
			x, y = y, x
		}
		e.toXMM(x, "%xmm0")
		e.toXMM(y, "%xmm1")
		op("ucomi%s\t%%xmm1, %%xmm0", sse(x.Type))
		switch in.Op {
		case ir.FEq:
			// Equal and ordered: ZF set and PF clear.
			op("sete\t%%al")
			op("setnp\t%%cl")
			op("andb\t%%cl, %%al")
		case ir.FNe:
			op("setne\t%%al")
			op("setp\t%%cl")
			op("orb\t%%cl, %%al")
		default:
			op("%s\t%%al", c.cc)
		}
		op("movzbl\t%%al, %%eax")
		op("movq\t%%rax, %s", e.val(in))
	case ir.IToF:
		// Every canonical integer but a U64 at or above 2^63 is its own
		// int64 value, which cvtsi2s converts with one rounding. Such a U64
		// is halved, keeping its lowest bit so the rounding stays correct,
		// converted, and doubled exactly.
		s := sse(in.Type)
		e.load(in.Args[0], "%rax")
		if in.Args[0].Type == ir.U64 {
			op("testq\t%%rax, %%rax")
			op("js\t1f")
			op("cvtsi2%sq\t%%rax, %%xmm0", s)
			op("jmp\t2f")
			e.b.WriteString("1:\n")
			op("movq\t%%rax, %%rcx")
			op("shrq\t$1, %%rcx")
			op("andl\t$1, %%eax")
			op("orq\t%%rax, %%rcx")
			op("cvtsi2%sq\t%%rcx, %%xmm0", s)
			op("add%s\t%%xmm0, %%xmm0", s)
			e.b.WriteString("2:\n")
		} else {
			op("cvtsi2%sq\t%%rax, %%xmm0", s)
		}
		e.fromXMM(in, "%xmm0")
	case ir.FToF:
		e.toXMM(in.Args[0], "%xmm0")
		if in.Args[0].Type != in.Type {
			op("cvt%s2%s\t%%xmm0, %%xmm0", sse(in.Args[0].Type), sse(in.Type))
		}
		e.fromXMM(in, "%xmm0")
	case ir.FToI:
		// An F32 converts to float64 exactly first; the conversions are
		// then those of FToI's definition.
		e.toXMM(in.Args[0], "%xmm0")
		if in.Args[0].Type == ir.F32 {
			op("cvtss2sd\t%%xmm0, %%xmm0")
		}
		switch in.Type {
		case ir.I64, ir.U32:
			op("cvttsd2si\t%%xmm0, %%rax")
		case ir.U64:
			op("movabsq\t$0x43e0000000000000, %%rcx") // 2^63
			op("movq\t%%rcx, %%xmm1")
			op("ucomisd\t%%xmm1, %%xmm0")
			op("jae\t1f")
			// Below 2^63, or NaN, which truncates to bit 63 alone either way.
			op("cvttsd2si\t%%xmm0, %%rax")
			op("jmp\t2f")
			e.b.WriteString("1:\n")
			op("subsd\t%%xmm1, %%xmm0")
			op("cvttsd2si\t%%xmm0, %%rax")
			op("btsq\t$63, %%rax")
			e.b.WriteString("2:\n")
		default:
			op("cvttsd2si\t%%xmm0, %%eax")
		}
		canon(op, in.Type)
		op("movq\t%%rax, %s", e.val(in))
	default:
		return fmt.Errorf("no instruction selection for %s", in.Op)
	}
	return nil
}
