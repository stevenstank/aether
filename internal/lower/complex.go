package lower

import (
	"go/ast"
	"go/constant"
	"go/types"
	"math"

	"github.com/stevenstank/aether/internal/ir"
)

// Complex numbers (D-049, increment 11.8) are values without arithmetic,
// lowered as aggregates of their two parts, as gc lays them out: the real
// part, then the imaginary part, each a float32 of a complex64 and a
// float64 of a complex128. Equality, copies, and hashes are those of the
// parts.

func isComplex(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsComplex != 0
}

// complexPart returns the type of the parts of the complex type t, and
// their size.
func complexPart(t types.Type) (types.Type, int64) {
	if t.Underlying().(*types.Basic).Kind() == types.Complex64 {
		return types.Typ[types.Float32], 4
	}
	return types.Typ[types.Float64], 8
}

// complexValue returns the Ptr of a new memory object holding the value
// of e, of complex type, if e is a constant, a call of complex, or a
// conversion between complex types.
func (l *lowerer) complexValue(e ast.Expr) (*ir.Instr, bool) {
	tv := l.pkg.Info.Types[e]
	t := tv.Type
	pos := l.pos(e, e.Pos())
	pt, size := complexPart(t)
	ft := l.irType(e, pt)
	if tv.Value != nil {
		re, _ := constant.Float64Val(constant.ToFloat(constant.Real(tv.Value)))
		im, _ := constant.Float64Val(constant.ToFloat(constant.Imag(tv.Value)))
		return l.complexOf(t, func() *ir.Instr { return l.fn.FloatConst(l.b, ft, re, pos) },
			func() *ir.Instr { return l.fn.FloatConst(l.b, ft, im, pos) }, pos), true
	}
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	if l.isBuiltin(call, "complex") {
		re := l.keep(l.scalar(call.Args[0], l.valueFor(call.Args[0], pt)), pos)
		im := l.scalar(call.Args[1], l.valueFor(call.Args[1], pt))
		return l.complexOf(t, re, func() *ir.Instr { return im }, pos), true
	}
	if tv := l.pkg.Info.Types[call.Fun]; tv.IsType() {
		// A conversion from another complex type: of each part.
		from := l.pkg.Info.Types[call.Args[0]].Type
		fpt, fsize := complexPart(from)
		src := l.keep(l.aggValue(call.Args[0]), pos)
		part := func(off int64) func() *ir.Instr {
			return func() *ir.Instr {
				x := l.fn.LdM(l.b, l.irType(e, fpt), src(), l.fn.Const(l.b, off, pos), pos)
				if size != fsize {
					x = l.fn.Convert(l.b, ir.FToF, ft, x, pos)
				}
				return x
			}
		}
		return l.complexOf(t, part(0), part(fsize), pos), true
	}
	return nil, false
}

// complexOf returns the Ptr of a new memory object holding the complex
// number of type t with the parts re and im, evaluated in that order.
func (l *lowerer) complexOf(t types.Type, re, im func() *ir.Instr, pos ir.Pos) *ir.Instr {
	_, size := complexPart(t)
	m := l.newMem("complex", t, pos)
	l.fn.StM(l.b, l.addr(m, pos), l.fn.Const(l.b, 0, pos), re(), pos)
	l.fn.StM(l.b, l.addr(m, pos), l.fn.Const(l.b, size, pos), im(), pos)
	return l.addr(m, pos)
}

// complexPartOf lowers real(c) or imag(c), of a complex c that is not a
// constant.
func (l *lowerer) complexPartOf(call *ast.CallExpr, imag bool) *ir.Instr {
	pos := l.pos(call, call.Pos())
	ct := l.pkg.Info.Types[call.Args[0]].Type
	pt, size := complexPart(ct)
	off := int64(0)
	if imag {
		off = size
	}
	return l.fn.LdM(l.b, l.irType(call, pt), l.aggValue(call.Args[0]), l.fn.Const(l.b, off, pos), pos)
}

// printComplex writes the complex panic value of rec, of type t, as gc
// does (strconv.FormatComplex): (re+imi), after the type's name if it is
// a defined type. The imaginary part has a sign: its own, or + if its text
// has none (a positive number, +0, or NaN).
func (l *lowerer) printComplex(t types.Type, rec func() *ir.Instr, pos ir.Pos) {
	if _, named := t.(*types.Named); named {
		l.printStr(l.gcName(t), pos)
	}
	pt, size := complexPart(t)
	ft := l.irType(nil, pt)
	part := func(off int64) *ir.Instr {
		return l.fn.LdM(l.b, ft, l.recField(rec, ir.Ptr, recData, pos), l.fn.Const(l.b, off, pos), pos)
	}
	l.printStr("(", pos)
	l.fn.Print(l.b, []*ir.Instr{part(0)}, pos)
	im := func() *ir.Instr { return l.fn.Convert(l.b, ir.FToF, ir.F64, part(size), pos) }
	// Its text starts with - if its sign bit is set and it is not NaN,
	// and with + if it is +Inf.
	done, plus := l.fn.NewBlock(), l.fn.NewBlock()
	bits := l.reinterpretFloat(im(), pos)
	nonneg := l.when(l.fn.Compare(l.b, ir.Lt, bits, l.fn.Const(l.b, 0, pos), pos), pos)
	nan := l.when(l.fn.Float(l.b, ir.FEq, pos, im(), im()), pos)
	l.fn.Jump(l.b, done, pos) // negative: its own -
	l.b = nan
	l.fn.Jump(l.b, plus, pos)
	l.b = nonneg
	finite := l.when(l.fn.Float(l.b, ir.FEq, pos, im(), l.fn.FloatConst(l.b, ir.F64, math.Inf(1), pos)), pos)
	l.fn.Jump(l.b, done, pos) // +Inf
	l.b = finite
	l.fn.Jump(l.b, plus, pos)
	l.b = plus
	l.printStr("+", pos)
	l.fn.Jump(l.b, done, pos)
	l.b = done
	l.fn.Print(l.b, []*ir.Instr{part(size)}, pos)
	l.printStr("i)", pos)
}

// reinterpretFloat returns the bits of the float64 x as an I64.
func (l *lowerer) reinterpretFloat(x *ir.Instr, pos ir.Pos) *ir.Instr {
	m := l.fn.NewMem("bits", 8, 8, pos)
	a, zero := l.fn.Addr(l.b, m, pos), l.fn.Const(l.b, 0, pos)
	l.fn.StM(l.b, a, zero, x, pos)
	return l.fn.LdM(l.b, ir.I64, a, zero, pos)
}
