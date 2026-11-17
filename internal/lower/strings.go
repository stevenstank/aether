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

// stringConversion lowers string(x) of a string, which is x, of a []byte,
// a copy of its elements, or of a []rune or an integer (runes.go).
func (l *lowerer) stringConversion(call *ast.CallExpr) []*ir.Instr {
	pos := l.pos(call, call.Pos())
	at := l.pkg.Info.Types[call.Args[0]].Type
	switch {
	case isString(at):
		return l.value(call.Args[0])
	case isRuneSlice(at):
		return l.runesStringConversion(call)
	case isSlice(at):
		w := l.value(call.Args[0])
		return []*ir.Instr{l.copyBytes(w[0], w[1], pos), w[1]}
	case isInteger(at):
		return l.runeConversion(call)
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

// genPrefix begins the IR name of every function lowering generates: .streq,
// .strcmp, and the UTF-8 functions (D-037), with symbols main..streq and so
// on, as gc names its own generated symbols. The program's functions are
// named F and its methods T.M (D-038), by Go identifiers, which are never
// empty, so none of their names begins with a dot: the generated names are
// disjoint from them whatever the program names its types and functions.
const genPrefix = "."

// streqName is the name of the generated string equality function.
const streqName = genPrefix + "streq"

// streqFunc returns the program's string equality function, which lowering
// adds on its first use (D-037):
//
//	func .streq(p ptr, n i64, q ptr, m i64) bool
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
// equal: a call of .streq, which does not branch in the caller.
func (l *lowerer) stringEqual(x, y []*ir.Instr, pos ir.Pos) *ir.Instr {
	return l.fn.Call(l.b, l.streqFunc(pos), []*ir.Instr{x[0], x[1], y[0], y[1]}, pos)
}

// stringCompare lowers a comparison of strings: both are evaluated, left
// to right, then compared, for == and != by .streq, and for < <= >
// >= by .strcmp.
func (l *lowerer) stringCompare(e *ast.BinaryExpr, pos ir.Pos) *ir.Instr {
	xy := l.values([]ast.Expr{e.X, e.Y}, nil)
	x, y := xy[0], xy[1]
	switch e.Op {
	case token.EQL:
		return l.stringEqual(x, y, pos)
	case token.NEQ:
		return l.fn.Not(l.b, l.stringEqual(x, y, pos), pos)
	}
	op, ok := binaryOps[e.Op]
	if !ok || !op.IsComparison() {
		l.fail(e, "string operator %s", e.Op)
	}
	c := l.fn.Call(l.b, l.strcmpFunc(pos), []*ir.Instr{x[0], x[1], y[0], y[1]}, pos)
	return l.fn.Compare(l.b, op, c, l.fn.Const(l.b, 0, pos), pos)
}

// strcmpName is the name of the generated string ordering function.
const strcmpName = genPrefix + "strcmp"

// strcmpFunc returns the program's string ordering function, which lowering
// adds on its first use (D-037):
//
//	func .strcmp(p ptr, n i64, q ptr, m i64) i64
//
// It compares the n bytes at p with the m bytes at q lexicographically, as
// unsigned bytes, a proper prefix first, as Go orders strings, and returns
// -1, 0, or 1 as the first is less than, equal to, or greater than the
// second.
func (l *lowerer) strcmpFunc(pos ir.Pos) *ir.Func {
	if l.strcmp != nil {
		return l.strcmp
	}
	f := l.prog.NewFunc(strcmpName, l.fn.File, pos)
	f.Params = []ir.Type{ir.Ptr, ir.I64, ir.Ptr, ir.I64}
	f.Results = []ir.Type{ir.I64}
	g := gen{f, pos}
	entry := f.Entry()
	ps, qs, ns, ms, is := g.slot("p", ir.Ptr), g.slot("q", ir.Ptr), g.slot("n", ir.I64), g.slot("m", ir.I64), g.slot("i", ir.I64)
	p, n, q, m := g.param(entry, 0), g.param(entry, 1), g.param(entry, 2), g.param(entry, 3)
	g.store(entry, ps, p)
	g.store(entry, qs, q)
	g.store(entry, ns, n)
	g.store(entry, ms, m)
	g.store(entry, is, g.c(entry, 0))
	head, inM, body, differ, next, tail, longer := f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	less, greater, equal := g.ret(-1), g.ret(1), g.ret(0)
	f.Jump(entry, head, pos)
	// for i < n && i < m: the first differing byte decides.
	f.Branch(head, g.cmp(head, ir.Lt, g.ld(head, is), g.ld(head, ns)), inM, tail, pos)
	f.Branch(inM, g.cmp(inM, ir.Lt, g.ld(inM, is), g.ld(inM, ms)), body, tail, pos)
	bytes := func(b *ir.Block) (*ir.Instr, *ir.Instr) {
		return f.LoadB(b, g.ld(b, ps), g.ld(b, is), pos), f.LoadB(b, g.ld(b, qs), g.ld(b, is), pos)
	}
	a, b := bytes(body)
	f.Branch(body, g.cmp(body, ir.Lt, a, b), less, differ, pos)
	a, b = bytes(differ)
	f.Branch(differ, g.cmp(differ, ir.Gt, a, b), greater, next, pos)
	g.store(next, is, f.Binary(next, ir.Add, g.ld(next, is), g.c(next, 1), pos))
	f.Jump(next, head, pos)
	// One is a prefix of the other: the shorter is less.
	f.Branch(tail, g.cmp(tail, ir.Lt, g.ld(tail, ns), g.ld(tail, ms)), less, longer, pos)
	f.Branch(longer, g.cmp(longer, ir.Gt, g.ld(longer, ns), g.ld(longer, ms)), greater, equal, pos)
	l.strcmp = f
	return f
}
