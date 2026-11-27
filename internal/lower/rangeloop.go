package lower

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/stevenstank/aether/internal/ir"
)

// rangeStmt lowers a range over an integer, an array, a pointer to an
// array, a slice, or a string (D-049, increment 11.3), as the specification
// defines it:
//
//   - the range expression is evaluated once, before the loop, except that
//     an array or a pointer to one is not evaluated when there is no value
//     variable and it has no function call or receive (its length is then
//     constant);
//   - a hidden index runs from 0 below the length, or the integer; a
//     string's index steps over each rune decoded, U+FFFD and one byte for
//     invalid UTF-8;
//   - an array value is ranged over as a copy; a slice's, a pointer's, and
//     a string's elements are read each iteration;
//   - each iteration assigns or declares the key and the value, as an
//     assignment would; a declared variable is new in each iteration.
func (l *lowerer) rangeStmt(s *ast.RangeStmt) {
	pos := l.pos(s, s.For)
	xt := l.pkg.Info.Types[s.X].Type
	key, val := rangeVar(s.Key), rangeVar(s.Value)
	it := ir.I64 // the hidden index's IR type
	// The types of the key and the value, which an assignment converts
	// to its targets' (spec §For statements with range clause).
	keyT, valT := types.Type(types.Typ[types.Int]), types.Type(nil)
	var n func() *ir.Instr
	var elem func(i func() *ir.Instr) place // the element at index i
	var ptr func() *ir.Instr                // a string's bytes
	switch u := xt.Underlying().(type) {
	case *types.Basic:
		if u.Info()&types.IsString != 0 {
			w := l.value(s.X)
			ptr, n = l.keep(w[0], pos), l.keep(w[1], pos)
			valT = types.Universe.Lookup("rune").Type()
			break
		}
		// An integer: the key's type, which is the operand's, or int
		// for an untyped constant without a declared key.
		kt := types.Default(xt)
		if key != nil && s.Tok == token.DEFINE {
			kt = l.pkg.Info.Defs[key.(*ast.Ident)].Type()
		}
		it, keyT = l.irType(s.X, kt), kt
		n = l.keep(l.scalar(s.X, l.valueFor(s.X, kt)), pos)
	case *types.Slice:
		w := l.value(s.X)
		base, length := l.keep(w[0], pos), l.keep(w[1], pos)
		n, valT = length, u.Elem()
		elem = func(i func() *ir.Instr) place {
			return l.indexed(place{base: base, t: u.Elem()}, i, sizes.Sizeof(u.Elem()), u.Elem(), pos)
		}
	default:
		at, isPtr := pointee(xt)
		if !isPtr {
			at = xt
		}
		a, ok := at.Underlying().(*types.Array)
		if !ok {
			l.fail(s, "range over %s", xt)
		}
		length := a.Len()
		n, valT = func() *ir.Instr { return l.fn.Const(l.b, length, pos) }, a.Elem()
		var arr func() place
		switch {
		case val == nil:
			if rangeEvaluates(l.pkg.Info, s.X) {
				l.value(s.X) // evaluated for its effects only
			}
		case isPtr:
			p := l.keep(l.expr(s.X), pos)
			arr = func() place { return l.through(p(), at, pos) }
		default:
			// A copy, so that assignments in the body are not seen.
			m := l.newMem("range", at, pos)
			l.copyPlace(l.memPlace(m, at, pos), l.valuePlace(l.aggValue(s.X), at, pos), pos)
			arr = func() place { return l.memPlace(m, at, pos) }
		}
		elem = func(i func() *ir.Instr) place {
			return l.indexed(arr(), i, sizes.Sizeof(a.Elem()), a.Elem(), pos)
		}
	}
	hi := l.fn.NewSlot("range.i", it, pos)
	l.fn.Store(l.b, hi, l.fn.TypedConst(l.b, it, 0, pos), pos)
	var after *ir.Slot // the offset after a string's rune
	if ptr != nil {
		after = l.fn.NewSlot("range.next", ir.I64, pos)
	}
	head, body, next, exit := l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Jump(l.b, head, pos)
	l.b = head
	l.backEdge(pos)
	l.fn.Branch(l.b, l.compare(ir.Lt, l.fn.Load(l.b, hi, pos), n(), pos), body, exit, pos)
	l.b, l.done = body, false

	var targets []ast.Expr
	var vals []func() []*ir.Instr
	add := func(x ast.Expr, w []*ir.Instr, from types.Type) {
		xpos := l.pos(x, x.Pos())
		if s.Tok == token.DEFINE {
			l.store(l.defineVar(l.ident(x)), w, xpos)
			return
		}
		targets = append(targets, x)
		vals = append(vals, l.spillWords(l.assignTo(w, from, x, xpos), xpos))
	}
	var r *ir.Instr // a string's rune
	if ptr != nil {
		d := l.fn.Call(l.b, l.runeFunc(decodeRune, pos), []*ir.Instr{ptr(), n(), l.fn.Load(l.b, hi, pos)}, pos)
		r = l.fn.Extract(l.b, d, 0, pos)
		l.fn.Store(l.b, after, l.fn.Extract(l.b, d, 1, pos), pos)
		r = l.fn.Conv(l.b, ir.I32, r, pos)
	}
	if key != nil {
		add(key, []*ir.Instr{l.fn.Load(l.b, hi, pos)}, keyT)
	}
	if val != nil {
		// The index is loaded where the element's address is computed,
		// which may be after a nil check's branch.
		idx := func() *ir.Instr { return l.fn.Load(l.b, hi, pos) }
		var w []*ir.Instr
		switch {
		case ptr != nil:
			w = []*ir.Instr{r}
		case isAggregate(l.pkg.Info.TypeOf(val)) && s.Tok == token.DEFINE:
			w = []*ir.Instr{l.pointer(elem(idx), pos)}
		default:
			w = l.loadPlace(elem(idx), pos)
		}
		add(val, w, valT)
	}
	// As an assignment: the targets' operands, then the stores.
	vs := make([]variable, len(targets))
	for k, x := range targets {
		vs[k] = l.lvalue(x)
	}
	for k, x := range targets {
		l.store(vs[k], vals[k](), l.pos(x, x.Pos()))
	}

	lp := &target{next: next, exit: exit, stmt: s}
	l.targets = append(l.targets, lp)
	l.stmts(s.Body.List)
	if !l.done {
		l.fn.Jump(l.b, next, l.pos(s.Body, s.Body.Rbrace))
	}
	l.targets = l.targets[:len(l.targets)-1]
	l.b, l.done = next, false
	if after != nil {
		l.fn.Store(l.b, hi, l.fn.Load(l.b, after, pos), pos)
	} else {
		l.fn.Store(l.b, hi, l.fn.Binary(l.b, ir.Add, l.fn.Load(l.b, hi, pos), l.fn.TypedConst(l.b, it, 1, pos), pos), pos)
	}
	l.fn.Jump(l.b, head, pos)
	l.b, l.done = exit, false
}

// rangeVar returns the key or value expression x of a range clause, or nil
// if it is absent or blank.
func rangeVar(x ast.Expr) ast.Expr {
	if x == nil || isBlank(x) {
		return nil
	}
	return x
}

// rangeEvaluates reports whether a range over the array or pointer to an
// array x, without a value variable, evaluates x: when x has a function
// call that is not constant or a receive, so that len(x) is not constant
// (spec §For statements with range clause, §Length and capacity).
func rangeEvaluates(info *types.Info, x ast.Expr) bool {
	found := false
	ast.Inspect(x, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if tv := info.Types[n]; tv.Value == nil && !info.Types[n.Fun].IsType() {
				found = true
			}
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				found = true
			}
		case *ast.FuncLit:
			return false
		}
		return !found
	})
	return found
}
