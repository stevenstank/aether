package lower

import (
	"go/ast"
	"go/types"

	"github.com/stevenstank/aether/internal/ir"
)

// The unsafe package (D-049, increment 11.7). An unsafe.Pointer is a Ptr,
// as every pointer is; Sizeof, Alignof, and Offsetof are constants, which
// go/types computes with the layout lowering gives (D-034).

// unsafeCall lowers e if it is a call of Add, String, StringData, Slice,
// or SliceData of package unsafe, returning its words.
func (l *lowerer) unsafeCall(e ast.Expr) ([]*ir.Instr, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || l.pkg.Info.Types[call].Value != nil {
		return nil, false // Sizeof, Alignof, and Offsetof are constants
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return nil, false
	}
	b, ok := l.pkg.Info.Uses[id].(*types.Builtin)
	if !ok || types.Unsafe.Scope().Lookup(b.Name()) != b {
		return nil, false
	}
	pos := l.pos(call, call.Pos())
	switch b.Name() {
	case "StringData", "SliceData":
		// The data's address: the first word (D-033, D-036).
		return l.value(call.Args[0])[:1], true
	}
	p := l.keep(l.expr(call.Args[0]), pos)
	n := l.fn.Conv(l.b, ir.I64, l.expr(call.Args[1]), pos)
	switch b.Name() {
	case "Add":
		return []*ir.Instr{l.fn.PAdd(l.b, p(), n, pos)}, true
	case "String":
		nk := l.keep(n, pos)
		l.unsafeCheck(p, nk, 1, "unsafe.String", pos)
		return []*ir.Instr{p(), nk()}, true
	case "Slice":
		nk := l.keep(n, pos)
		et := call.Args[0]
		l.unsafeCheck(p, nk, sizes.Sizeof(l.pkg.Info.TypeOf(et).Underlying().(*types.Pointer).Elem()), "unsafe.Slice", pos)
		return []*ir.Instr{p(), nk(), nk()}, true
	}
	l.fail(call, "call of unsafe.%s", b.Name())
	return nil, false
}

// unsafeCheck panics as gc's unsafe.String and unsafe.Slice do if n
// elements of size bytes at p are not valid: with a negative n, or if p is
// nil and n is not zero, or if they would extend past the end of the
// address space.
func (l *lowerer) unsafeCheck(p, n func() *ir.Instr, size int64, what string, pos ir.Pos) {
	m := mb{l, pos}
	fail := func(msg string) {
		l.panicMsg([][]*ir.Instr{l.strWords("runtime error: "+what+": "+msg, pos)}, pos)
	}
	isNil := func() *ir.Instr { return m.cmp(ir.Eq, p(), m.nilPtr()) }
	ok := m.when(m.cmp(ir.Lt, n(), m.c(0)))
	fail("len out of range")
	l.b = ok
	done := l.fn.NewBlock()
	if size == 0 {
		// No bytes: only a nil p with a positive n is invalid.
		other := m.when(isNil())
		zero := m.when(m.cmp(ir.Gt, n(), m.c(0)))
		fail("ptr is nil and len is not zero")
		l.b = zero
		m.join(done)
		l.b = other
		m.join(done)
		return
	}
	// The n·size bytes must not overflow, or exceed -uintptr(p).
	bad := l.fn.NewBlock()
	nu := func() *ir.Instr { return l.fn.Conv(l.b, ir.U64, n(), pos) }
	if size > 1 {
		next := l.fn.NewBlock()
		max := l.fn.TypedConst(l.b, ir.U64, int64(^uint64(0)/uint64(size)), pos)
		l.fn.Branch(l.b, m.cmp(ir.Gt, nu(), max), bad, next, pos)
		l.b = next
	}
	bytes := m.bin(ir.Mul, nu(), l.fn.TypedConst(l.b, ir.U64, size, pos))
	room := l.fn.Neg(l.b, l.ptrToInt(p(), pos), pos)
	l.fn.Branch(l.b, m.cmp(ir.Gt, bytes, room), bad, done, pos)
	l.b = bad
	nonNil := m.when(isNil())
	fail("ptr is nil and len is not zero")
	l.b = nonNil
	fail("len out of range")
	l.b = done
}

// ptrToInt returns the address p as a uintptr, through a memory object.
func (l *lowerer) ptrToInt(p *ir.Instr, pos ir.Pos) *ir.Instr {
	return l.reinterpret(p, ir.U64, pos)
}

// reinterpret returns the bits of x, a Ptr or a 64-bit integer, as type t,
// the other: stored in a memory object and loaded as t.
func (l *lowerer) reinterpret(x *ir.Instr, t ir.Type, pos ir.Pos) *ir.Instr {
	m := l.fn.NewMem("bits", 8, 8, pos)
	a, zero := l.fn.Addr(l.b, m, pos), l.fn.Const(l.b, 0, pos)
	l.fn.StM(l.b, a, zero, x, pos)
	return l.fn.LdM(l.b, t, a, zero, pos)
}
