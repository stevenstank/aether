package lower

// Arrays and structs (Phase 6 increment 6.3, D-034).
//
// Every array or struct variable, global, and temporary is a memory object
// laid out by types.SizesFor("gc", "amd64"). An array or struct value, as a
// Go value lowering passes around, is one word: the Ptr of a temporary object
// that holds it and that nothing else writes while the value is in use, so
// a consumer may take it over. A parameter of such a type is the Ptr of a
// copy its caller made for the call; a result is written, before the
// return, into an object whose Ptr the caller passes after the other
// arguments.
//
// A place is memory holding a Go value: a variable, or a field or element
// of one, addressed in place. Its parts are functions that produce their
// value in the current block, reusing it there and reloading it from a
// temporary slot elsewhere, so a place stays usable across the blocks that
// bounds checks and copy loops create.
//
// Copies, zeroing, and comparisons go leaf by leaf: each scalar word of the
// value, two for a string. An array of more than unrollLeaves leaves is
// handled by a loop over its elements; padding is never touched.

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/stevenstank/aether/internal/ir"
)

// unrollLeaves is the most leaves an array is copied, zeroed, or compared
// with straight-line code rather than a loop.
const unrollLeaves = 16

// sizes is the layout of arrays and structs (D-034), the gc/amd64 one that
// go/types reports for unsafe.Sizeof and unsafe.Offsetof.
var sizes = types.SizesFor("gc", "amd64")

func isAggregate(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Array, *types.Struct:
		return true
	}
	return false
}

// leaves returns the number of scalar words of a value of t, or
// unrollLeaves+1 if it is larger.
func leaves(t types.Type) int64 {
	switch u := t.Underlying().(type) {
	case *types.Array:
		n := leaves(u.Elem())
		if n > 0 && u.Len() > unrollLeaves {
			return unrollLeaves + 1
		}
		return min(n*u.Len(), unrollLeaves+1)
	case *types.Struct:
		var n int64
		for f := range u.Fields() {
			n = min(n+leaves(f.Type()), unrollLeaves+1)
		}
		return n
	}
	if isString(t) {
		return 2
	}
	return 1
}

// mayLoop reports whether copying, zeroing, or comparing a value of t may
// create blocks.
func mayLoop(t types.Type) bool { return leaves(t) > unrollLeaves }

// place is the memory holding a Go value of type t: the object at base, at
// byte offset dyn + c; dyn is nil if the offset is a constant. checks are
// the bounds checks of the index expressions selecting it, if an assignment
// to it has deferred them (spec §Assignment statements); they come before
// any access.
type place struct {
	base   func() *ir.Instr
	dyn    func() *ir.Instr
	c      int64
	t      types.Type
	checks []boundsCheck
}

// boundsCheck is a deferred check that the index idx is below n.
type boundsCheck struct {
	idx func() *ir.Instr
	n   int64
	pos ir.Pos
}

// field returns the part of p at offset off, of type t.
func (p place) field(off int64, t types.Type) place {
	q := p
	q.c, q.t = p.c+off, t
	return q
}

// keep returns a function that produces v in the current block: v itself in
// the block where v was computed, and elsewhere its address again, for an
// addr, or a load of the temporary slot keep stores it in.
func (l *lowerer) keep(v *ir.Instr, pos ir.Pos) func() *ir.Instr {
	blk := l.b
	var again func() *ir.Instr
	if v.Op == ir.Addr {
		m := v.Mem
		again = func() *ir.Instr { return l.addr(m, pos) }
	} else {
		again = l.spill(v, pos)
	}
	return func() *ir.Instr {
		if l.b == blk {
			return v
		}
		return again()
	}
}

// addr returns the address of m in the current block, computed once there.
func (l *lowerer) addr(m *ir.Mem, pos ir.Pos) *ir.Instr {
	if l.addrBlock != l.b {
		l.addrBlock, l.addrs = l.b, map[*ir.Mem]*ir.Instr{}
	}
	if a := l.addrs[m]; a != nil {
		return a
	}
	a := l.fn.Addr(l.b, m, pos)
	l.addrs[m] = a
	return a
}

// newMem returns a new local memory object for a value of t.
func (l *lowerer) newMem(name string, t types.Type, pos ir.Pos) *ir.Mem {
	return l.fn.NewMem(name, sizes.Sizeof(t), sizes.Alignof(t), pos)
}

// memPlace returns the place of the whole object m, of type t.
func (l *lowerer) memPlace(m *ir.Mem, t types.Type, pos ir.Pos) place {
	return place{base: func() *ir.Instr { return l.addr(m, pos) }, t: t}
}

// valuePlace returns the place of an array or struct value, the Ptr v.
func (l *lowerer) valuePlace(v *ir.Instr, t types.Type, pos ir.Pos) place {
	return place{base: l.keep(v, pos), t: t}
}

// offset returns p's byte offset in the current block.
func (l *lowerer) offset(p place, pos ir.Pos) *ir.Instr {
	switch {
	case p.dyn == nil:
		return l.fn.Const(l.b, p.c, pos)
	case p.c == 0:
		return p.dyn()
	}
	d := p.dyn()
	return l.fn.Binary(l.b, ir.Add, d, l.fn.Const(l.b, p.c, pos), pos)
}

// ldm loads the scalar t at p.
func (l *lowerer) ldm(t ir.Type, p place, pos ir.Pos) *ir.Instr {
	return l.fn.LdM(l.b, t, p.base(), l.offset(p, pos), pos)
}

// stm stores the scalar v at p.
func (l *lowerer) stm(p place, v *ir.Instr, pos ir.Pos) {
	l.fn.StM(l.b, p.base(), l.offset(p, pos), v, pos)
}

// walk calls leaf for each scalar word of a value of type t at the places
// ps, in order of offset, each place's part at the same offset; with blank,
// it skips blank fields. An array of more than unrollLeaves leaves is a
// loop over its elements. leaf may create blocks.
func (l *lowerer) walk(t types.Type, ps []place, blank bool, pos ir.Pos, leaf func(ir.Type, []place)) {
	at := func(off int64, t types.Type) []place {
		qs := make([]place, len(ps))
		for i, p := range ps {
			qs[i] = p.field(off, t)
		}
		return qs
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		var fields []*types.Var
		for f := range u.Fields() {
			fields = append(fields, f)
		}
		offs := sizes.Offsetsof(fields)
		for i, f := range fields {
			if !blank || f.Name() != "_" {
				l.walk(f.Type(), at(offs[i], f.Type()), blank, pos, leaf)
			}
		}
	case *types.Array:
		et, n := u.Elem(), u.Len()
		es := sizes.Sizeof(et)
		if n == 0 || leaves(et) == 0 {
			return
		}
		if !mayLoop(t) {
			for k := range n {
				l.walk(et, at(k*es, et), blank, pos, leaf)
			}
			return
		}
		// for i := 0; i < n; i++ { the element at i*es }
		i := l.fn.NewSlot("i", ir.I64, pos)
		l.fn.Store(l.b, i, l.fn.Const(l.b, 0, pos), pos)
		head, body, exit := l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock()
		l.fn.Jump(l.b, head, pos)
		l.b = head
		l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Lt, l.fn.Load(l.b, i, pos), l.fn.Const(l.b, n, pos), pos), body, exit, pos)
		l.b = body
		qs := make([]place, len(ps))
		for k, p := range ps {
			qs[k] = l.indexed(p, func() *ir.Instr { return l.fn.Load(l.b, i, pos) }, es, et, pos)
		}
		l.walk(et, qs, blank, pos, leaf)
		l.fn.Store(l.b, i, l.fn.Binary(l.b, ir.Add, l.fn.Load(l.b, i, pos), l.fn.Const(l.b, 1, pos), pos), pos)
		l.fn.Jump(l.b, head, pos)
		l.b = exit
	default:
		if isString(t) {
			leaf(ir.Ptr, at(0, t))
			leaf(ir.I64, at(8, t))
			return
		}
		leaf(l.irType(nil, t), ps)
	}
}

// indexed returns the element of type et at index idx, an I64 no larger
// than the array's length, of the array at p, whose elements are es bytes.
func (l *lowerer) indexed(p place, idx func() *ir.Instr, es int64, et types.Type, pos ir.Pos) place {
	q := p
	q.t = et
	q.dyn = func() *ir.Instr {
		x := idx()
		if es != 1 {
			x = l.fn.Binary(l.b, ir.Mul, x, l.fn.Const(l.b, es, pos), pos)
		}
		if p.dyn != nil {
			x = l.fn.Binary(l.b, ir.Add, p.dyn(), x, pos)
		}
		return x
	}
	return q
}

// copyPlace copies the value at src to dst, which are of the same type and
// are the same object or do not overlap.
func (l *lowerer) copyPlace(dst, src place, pos ir.Pos) {
	l.walk(dst.t, []place{dst, src}, false, pos, func(t ir.Type, ps []place) {
		l.stm(ps[0], l.ldm(t, ps[1], pos), pos)
	})
}

// zero stores the zero value of p's type at p: every leaf, the empty
// string's (0, 0) included, is zero bits.
func (l *lowerer) zero(p place, pos ir.Pos) {
	l.walk(p.t, []place{p}, false, pos, func(t ir.Type, ps []place) {
		l.stm(ps[0], l.fn.TypedConst(l.b, t, 0, pos), pos)
	})
}

// equal returns whether the values at x and y, of the same type, are equal:
// every pair of corresponding leaves, blank fields excepted, is equal, a
// float by IEEE 754 (spec §Comparison operators). The leaves are compared in
// order until one pair differs.
func (l *lowerer) equal(x, y place, pos ir.Pos) *ir.Instr {
	res := l.fn.NewSlot("tmp", ir.Bool, pos)
	differ := l.fn.NewBlock()
	l.walk(x.t, []place{x, y}, true, pos, func(t ir.Type, ps []place) {
		if t == ir.Ptr {
			l.fail(nil, "comparison of a string") // excluded by the gate
		}
		eq := l.compare(ir.Eq, l.ldm(t, ps[0], pos), l.ldm(t, ps[1], pos), pos)
		next := l.fn.NewBlock()
		l.fn.Branch(l.b, eq, next, differ, pos)
		l.b = next
	})
	join := l.fn.NewBlock()
	l.fn.Store(l.b, res, l.fn.BoolConst(l.b, true, pos), pos)
	l.fn.Jump(l.b, join, pos)
	// Without leaves, nothing branches to differ, and prune removes it.
	l.fn.Store(differ, res, l.fn.BoolConst(differ, false, pos), pos)
	l.fn.Jump(differ, join, pos)
	l.b = join
	return l.fn.Load(l.b, res, pos)
}

// loadPlace returns the words of the value at p: a scalar's, a string's two,
// or the Ptr of a temporary copy of an array or struct.
func (l *lowerer) loadPlace(p place, pos ir.Pos) []*ir.Instr {
	p = l.checkAll(p)
	switch {
	case isAggregate(p.t):
		m := l.newMem("tmp", p.t, pos)
		l.copyPlace(l.memPlace(m, p.t, pos), p, pos)
		return []*ir.Instr{l.addr(m, pos)}
	case isString(p.t):
		return []*ir.Instr{l.ldm(ir.Ptr, p, pos), l.ldm(ir.I64, p.field(8, p.t), pos)}
	}
	return []*ir.Instr{l.ldm(l.irType(nil, p.t), p, pos)}
}

// storePlace stores the words w, a value of p's type, at p, after p's
// deferred bounds checks.
func (l *lowerer) storePlace(p place, w []*ir.Instr, pos ir.Pos) {
	get := func() []*ir.Instr { return w }
	if len(p.checks) > 0 {
		get = l.spillWords(w, pos)
	}
	p = l.checkAll(p)
	w = get()
	switch {
	case isAggregate(p.t):
		l.copyPlace(p, l.valuePlace(w[0], p.t, pos), pos)
	case isString(p.t):
		l.stm(p, w[0], pos)
		l.stm(p.field(8, p.t), w[1], pos)
	default:
		l.stm(p, w[0], pos)
	}
}

// checkAll emits p's deferred bounds checks, in order, and returns p
// without them.
func (l *lowerer) checkAll(p place) place {
	for _, c := range p.checks {
		l.checkIndex(c.idx, c.n, c.pos)
	}
	p.checks = nil
	return p
}

// checkIndex checks that the index idx, of any integer type, is below the
// length n, as an unsigned number so that a negative index fails too, and
// panics with the index, as written, and the length if not (D-033).
func (l *lowerer) checkIndex(idx func() *ir.Instr, n int64, pos ir.Pos) {
	u := idx()
	if u.Type != ir.U64 {
		u = l.fn.Conv(l.b, ir.U64, u, pos)
	}
	fail := l.fn.Compare(l.b, ir.Ge, u, l.fn.TypedConst(l.b, ir.U64, n, pos), pos)
	panicking, ok := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Branch(l.b, fail, panicking, ok, pos)
	l.b = panicking
	l.fn.Panic(panicking, ir.PanicIndex, pos, idx(), l.fn.Const(l.b, n, pos))
	l.b = ok
}

// addrOf returns the place of e, an expression of array or struct type, or
// a field or element of one: a variable, or its field or element, in place;
// anything else is evaluated into a temporary. The index of an element is
// evaluated, and its bounds checked, as the expression is, unless lhs, for
// the target of an assignment, defers the checks to the place's use.
func (l *lowerer) addrOf(e ast.Expr, lhs bool) place {
	pos := l.pos(e, e.Pos())
	t := l.pkg.Info.Types[e].Type
	switch e := e.(type) {
	case *ast.ParenExpr:
		return l.addrOf(e.X, lhs)
	case *ast.Ident:
		if v := l.lookup(e); v.mem != nil {
			return *v.mem
		}
		l.fail(e, "%s is not an array or struct variable", e.Name)
	case *ast.SelectorExpr:
		sel := l.pkg.Info.Selections[e]
		if sel == nil || sel.Kind() != types.FieldVal || sel.Indirect() {
			l.fail(e, "unexpected selector %s", types.ExprString(e))
		}
		p := l.addrOf(e.X, lhs)
		// Through the embedded fields of the path, if promoted.
		for _, k := range sel.Index() {
			s, ok := p.t.Underlying().(*types.Struct)
			if !ok {
				l.fail(e, "selector of a non-struct")
			}
			var fields []*types.Var
			for f := range s.Fields() {
				fields = append(fields, f)
			}
			p = p.field(sizes.Offsetsof(fields)[k], fields[k].Type())
		}
		return p
	case *ast.IndexExpr:
		a, ok := l.pkg.Info.Types[e.X].Type.Underlying().(*types.Array)
		if !ok {
			l.fail(e, "index of a non-array")
		}
		p := l.addrOf(e.X, lhs)
		et, es := a.Elem(), sizes.Sizeof(a.Elem())
		if tv := l.pkg.Info.Types[e.Index]; tv.Value != nil {
			// go/types has checked a constant index against the length.
			k, exact := constantIndex(tv)
			if !exact || k < 0 || k >= a.Len() {
				l.fail(e, "constant index %s out of range", tv.Value)
			}
			return p.field(k*es, et)
		}
		i := l.expr(e.Index)
		if !i.Type.IsInteger() {
			l.fail(e.Index, "index of type %s", i.Type)
		}
		ipos := l.pos(e, e.Lbrack)
		idx := l.keep(i, ipos)
		if lhs {
			p.checks = append(append([]boundsCheck(nil), p.checks...), boundsCheck{idx, a.Len(), ipos})
		} else {
			l.checkIndex(idx, a.Len(), ipos)
		}
		// Below the length, so the same number as an I64.
		return l.indexed(p, func() *ir.Instr {
			x := idx()
			if x.Type != ir.I64 {
				x = l.fn.Conv(l.b, ir.I64, x, ipos)
			}
			return x
		}, es, et, pos)
	}
	if !isAggregate(t) {
		l.fail(e, "address of a non-aggregate expression %s", types.ExprString(e))
	}
	return l.valuePlace(l.aggValue(e), t, pos)
}

// aggValue lowers e, of array or struct type, and returns its value: the
// Ptr of a new temporary object holding it.
func (l *lowerer) aggValue(e ast.Expr) *ir.Instr {
	pos := l.pos(e, e.Pos())
	t := l.pkg.Info.Types[e].Type
	switch e := ast.Unparen(e).(type) {
	case *ast.CompositeLit:
		m := l.newMem("tmp", t, pos)
		p := l.memPlace(m, t, pos)
		if !l.covers(e) {
			l.zero(p, pos)
		}
		l.fill(p, e)
		return l.addr(m, pos)
	case *ast.CallExpr:
		if l.pkg.Info.Types[e.Fun].IsType() {
			// A conversion between types of identical underlying types,
			// so of the same layout.
			return l.aggValue(e.Args[0])
		}
		vals := l.results(e, l.call(e))
		if len(vals) != 1 || len(vals[0]) != 1 {
			l.fail(e, "call of %d results used as one value", len(vals))
		}
		return vals[0][0]
	}
	return l.loadPlace(l.addrOf(e, false), pos)[0]
}

// fill stores the elements of the literal lit at p, which holds zeros:
// those it lists, in source order. A nested literal is stored in place.
func (l *lowerer) fill(p place, lit *ast.CompositeLit) {
	switch u := p.t.Underlying().(type) {
	case *types.Struct:
		var fields []*types.Var
		for f := range u.Fields() {
			fields = append(fields, f)
		}
		offs := sizes.Offsetsof(fields)
		for i, elt := range lit.Elts {
			k := i
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				id, _ := kv.Key.(*ast.Ident)
				k = -1
				for j, f := range fields {
					if id != nil && f.Name() == id.Name {
						k = j
					}
				}
				if k < 0 {
					l.fail(kv, "unknown field %s", types.ExprString(kv.Key))
				}
				elt = kv.Value
			}
			l.fillElement(p.field(offs[k], fields[k].Type()), elt)
		}
	case *types.Array:
		es := sizes.Sizeof(u.Elem())
		var k int64
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				var exact bool
				k, exact = constantIndex(l.pkg.Info.Types[kv.Key])
				if !exact {
					l.fail(kv, "array literal key %s is not a constant index", types.ExprString(kv.Key))
				}
				elt = kv.Value
			}
			if k < 0 || k >= u.Len() {
				l.fail(elt, "array literal index %d out of range", k)
			}
			l.fillElement(p.field(k*es, u.Elem()), elt)
			k++
		}
	default:
		l.fail(lit, "composite literal of type %s", p.t)
	}
}

// covers reports whether the literal lit sets every byte of its value that
// is ever read: every field, none of them blank, or every element, each
// nested literal in turn covering its own. Otherwise the value is zeroed
// first.
func (l *lowerer) covers(lit *ast.CompositeLit) bool {
	n := int64(len(lit.Elts))
	switch u := l.pkg.Info.Types[lit].Type.Underlying().(type) {
	case *types.Struct:
		if n != int64(u.NumFields()) {
			return false
		}
		for f := range u.Fields() {
			if f.Name() == "_" {
				return false
			}
		}
	case *types.Array:
		// go/types rejects a repeated index, so n distinct ones below the
		// length are all of them.
		if n != u.Len() {
			return false
		}
	default:
		return false
	}
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			elt = kv.Value
		}
		if nested, ok := ast.Unparen(elt).(*ast.CompositeLit); ok && !l.covers(nested) {
			return false
		}
	}
	return true
}

// fillElement stores the element e of a composite literal at p, which holds
// zeros.
func (l *lowerer) fillElement(p place, e ast.Expr) {
	if lit, ok := ast.Unparen(e).(*ast.CompositeLit); ok {
		l.fill(p, lit)
		return
	}
	l.storePlace(p, l.value(e), l.pos(e, e.Pos()))
}

// constantIndex returns the integer value of a constant index or key.
func constantIndex(tv types.TypeAndValue) (int64, bool) {
	if tv.Value == nil {
		return 0, false
	}
	return int64Val(tv)
}

// aggregateCompare lowers x == y or x != y of arrays or structs. Both
// operands are evaluated, left to right, before they are compared.
func (l *lowerer) aggregateCompare(e *ast.BinaryExpr, pos ir.Pos) *ir.Instr {
	if e.Op != token.EQL && e.Op != token.NEQ {
		l.fail(e, "operator %s on arrays or structs", e.Op)
	}
	vals := l.values([]ast.Expr{e.X, e.Y})
	t := l.pkg.Info.Types[e.X].Type
	x := l.valuePlace(l.scalar(e.X, vals[0]), t, pos)
	y := l.valuePlace(l.scalar(e.Y, vals[1]), t, pos)
	eq := l.equal(x, y, pos)
	if e.Op == token.NEQ {
		return l.fn.Not(l.b, eq, pos)
	}
	return eq
}
