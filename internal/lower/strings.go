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

// streqName is the name of the generated string equality function. A Go
// identifier has no '.', so it differs from every function of the program.
const streqName = "aether.streq"

// streqFunc returns the program's string equality function, which lowering
// adds on its first use (D-037):
//
//	func aether.streq(p ptr, n i64, q ptr, m i64) bool
//
// It is true exactly when n = m and the n bytes at p and q are equal, byte
// by byte: the addresses are never compared, and a string of length 0
// equals every other, whatever its Ptr.
func (l *lowerer) streqFunc(pos ir.Pos) *ir.Func {
	if l.streq != nil {
		return l.streq
	}
	f := l.prog.NewFunc(streqName, l.fn.File, pos)
	f.Params = []ir.Type{ir.Ptr, ir.I64, ir.Ptr, ir.I64}
	f.Results = []ir.Type{ir.Bool}
	entry := f.Entry()
	head, body, next, no, yes := f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	ps, qs, ns, is := f.NewSlot("p", ir.Ptr, pos), f.NewSlot("q", ir.Ptr, pos), f.NewSlot("n", ir.I64, pos), f.NewSlot("i", ir.I64, pos)
	// The parameters first, as ABI-R1 requires (D-030).
	p, n, q, m := f.Param(entry, 0, pos), f.Param(entry, 1, pos), f.Param(entry, 2, pos), f.Param(entry, 3, pos)
	f.Store(entry, ps, p, pos)
	f.Store(entry, qs, q, pos)
	f.Store(entry, ns, n, pos)
	f.Store(entry, is, f.Const(entry, 0, pos), pos)
	f.Branch(entry, f.Compare(entry, ir.Eq, n, m, pos), head, no, pos)
	// for i < n: the bytes at i differ, or i++
	f.Branch(head, f.Compare(head, ir.Lt, f.Load(head, is, pos), f.Load(head, ns, pos), pos), body, yes, pos)
	i := f.Load(body, is, pos)
	differ := f.Compare(body, ir.Ne, f.LoadB(body, f.Load(body, ps, pos), i, pos), f.LoadB(body, f.Load(body, qs, pos), i, pos), pos)
	f.Branch(body, differ, no, next, pos)
	f.Store(next, is, f.Binary(next, ir.Add, f.Load(next, is, pos), f.Const(next, 1, pos), pos), pos)
	f.Jump(next, head, pos)
	f.Ret(no, pos, f.BoolConst(no, false, pos))
	f.Ret(yes, pos, f.BoolConst(yes, true, pos))
	l.streq = f
	return f
}

// stringEqual returns whether the strings x and y, two words each, are
// equal: a call of aether.streq, which does not branch in the caller.
func (l *lowerer) stringEqual(x, y []*ir.Instr, pos ir.Pos) *ir.Instr {
	return l.fn.Call(l.b, l.streqFunc(pos), []*ir.Instr{x[0], x[1], y[0], y[1]}, pos)
}

// stringCompare lowers x == y or x != y of strings: both are evaluated,
// left to right, then compared.
func (l *lowerer) stringCompare(e *ast.BinaryExpr, pos ir.Pos) *ir.Instr {
	xy := l.values([]ast.Expr{e.X, e.Y}, nil)
	eq := l.stringEqual(xy[0], xy[1], pos)
	if e.Op == token.NEQ {
		return l.fn.Not(l.b, eq, pos)
	}
	return eq
}
