package lower

// Strings made at run time (Phase 7 increment 7.3, D-037).
//
// A string is the Ptr of its bytes and its I64 length (D-033). Its bytes are
// a program string's or a heap object's, and are never written once the
// string exists: concatenation and the conversions between strings and
// []byte allocate a heap object for the result and move the bytes there,
// with no branch, and slicing a string shares its bytes. A []byte converted
// from a string has a capacity equal to its length, which gc rounds up
// (V-6).

import (
	"go/ast"
	"go/token"

	"github.com/stevenstank/aether/internal/ir"
)

// concatOperands appends to ops the operands of e, a string, as a chain of
// +: the non-constant sums are flattened, in lexical order, which is the
// order Go evaluates them in.
func (l *lowerer) concatOperands(e ast.Expr, ops []ast.Expr) []ast.Expr {
	if b, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && b.Op == token.ADD && l.pkg.Info.Types[b].Value == nil {
		return l.concatOperands(b.Y, l.concatOperands(b.X, ops))
	}
	return append(ops, e)
}

// concatExpr lowers a string + chain: every operand is evaluated, left to
// right, then their bytes are moved, in order, into one new object.
func (l *lowerer) concatExpr(e *ast.BinaryExpr) []*ir.Instr {
	ops := l.concatOperands(e, nil)
	return l.concat(l.values(ops, nil), l.pos(e, e.OpPos))
}

// concat returns a new string of the bytes of the strings ws, in order.
// The lengths are at most the heap's size, so their sum does not overflow
// for any number of operands a program can write; alloc fails beyond the
// heap.
func (l *lowerer) concat(ws [][]*ir.Instr, pos ir.Pos) []*ir.Instr {
	n := ws[0][1]
	for _, w := range ws[1:] {
		n = l.fn.Binary(l.b, ir.Add, n, w[1], pos)
	}
	p := l.fn.Alloc(l.b, n, pos)
	off := l.fn.Const(l.b, 0, pos)
	for i, w := range ws {
		// With a length of 0, move does nothing, whatever the Ptrs (D-036).
		l.fn.Move(l.b, l.fn.PAdd(l.b, p, off, pos), w[0], w[1], pos)
		if i < len(ws)-1 {
			off = l.fn.Binary(l.b, ir.Add, off, w[1], pos)
		}
	}
	return []*ir.Instr{p, n}
}

// copyBytes returns the Ptr of a new heap object holding the n bytes at p.
func (l *lowerer) copyBytes(p, n *ir.Instr, pos ir.Pos) *ir.Instr {
	q := l.fn.Alloc(l.b, n, pos)
	l.fn.Move(l.b, q, p, n, pos)
	return q
}

// stringConversion lowers string(x) of a string, which is x, or of a
// []byte, a copy of its elements.
func (l *lowerer) stringConversion(call *ast.CallExpr) []*ir.Instr {
	pos := l.pos(call, call.Pos())
	at := l.pkg.Info.Types[call.Args[0]].Type
	switch {
	case isString(at):
		return l.value(call.Args[0])
	case isSlice(at):
		w := l.value(call.Args[0])
		return []*ir.Instr{l.copyBytes(w[0], w[1], pos), w[1]}
	}
	l.fail(call, "conversion of %s to a string", at)
	return nil
}

// bytesConversion lowers []byte(s) of a string: a new array of its bytes,
// as long as it.
func (l *lowerer) bytesConversion(call *ast.CallExpr) []*ir.Instr {
	w := l.value(call.Args[0])
	return []*ir.Instr{l.copyBytes(w[0], w[1], l.pos(call, call.Pos())), w[1], w[1]}
}

// concatAssign lowers s += t of a string variable, field, or element: as
// for x op= y, the operands of s's index expressions, then t, then s's
// bounds checks and its load; then the new string is stored.
func (l *lowerer) concatAssign(s *ast.AssignStmt) {
	pos := l.pos(s.Lhs[0], s.Lhs[0].Pos())
	tpos := l.pos(s, s.TokPos)
	if v := l.lvalue(s.Lhs[0]); v.mem != nil {
		p := *v.mem
		y := l.value(s.Rhs[0])
		get := func() []*ir.Instr { return y }
		if len(p.checks) > 0 {
			get = l.spillWords(y, pos)
		}
		p = l.checkAll(p)
		x := l.loadPlace(p, pos)
		l.storePlace(p, l.concat([][]*ir.Instr{x, get()}, tpos), pos)
		return
	}
	// As for x op= y in assign: x is read first and kept across t.
	id := l.ident(s.Lhs[0])
	v := l.lookup(id)
	x := l.load(v, pos)
	get := func() []*ir.Instr { return x }
	if l.branches(s.Rhs) {
		get = l.spillWords(x, pos)
	}
	y := l.value(s.Rhs[0])
	l.store(v, l.concat([][]*ir.Instr{get(), y}, tpos), pos)
}
