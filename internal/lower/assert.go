package lower

// Type assertions, type switches, and comparisons of interface values
// (Phase 8 increment 8.4, D-041).

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"github.com/stevenstank/aether/internal/ir"
)

// ifaceTest is how a type assertion or a type switch case tests the
// interface value tab, data of static type xt for the type t: it branches
// to yes with the words of the value as a t, made by bind in yes, or to no.
func (l *lowerer) ifaceTest(xt, t types.Type, tab, data func() *ir.Instr, yes, no *ir.Block, pos ir.Pos) (bind func() []*ir.Instr) {
	if !isIface(t) {
		var want *ir.Instr
		if isEmptyIface(xt) {
			want = l.addr(l.desc(t).mem, pos)
		} else {
			want = l.addr(l.itab(t, xt, pos).mem, pos)
		}
		l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, tab(), want, pos), yes, no, pos)
		return func() []*ir.Instr {
			if isDirect(t) {
				return []*ir.Instr{data()}
			}
			return l.loadPlace(l.valuePlace(data(), t, pos), pos)
		}
	}
	zero := l.fn.TypedConst(l.b, ir.Ptr, 0, pos)
	if isEmptyIface(t) || types.Identical(t.Underlying(), xt.Underlying()) {
		l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Ne, tab(), zero, pos), yes, no, pos)
		return func() []*ir.Instr {
			if types.Identical(t.Underlying(), xt.Underlying()) {
				return []*ir.Instr{tab(), data()}
			}
			return []*ir.Instr{l.dynDesc(xt, tab(), pos), data()}
		}
	}
	chk := l.fn.NewBlock()
	l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Ne, tab(), zero, pos), chk, no, pos)
	l.b = chk
	find := func() *ir.Instr {
		d := l.dynDesc(xt, tab(), pos)
		return l.fn.Call(l.b, l.findFunc(t, pos), []*ir.Instr{d, d}, pos)
	}
	l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Ne, find(), l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos), yes, no, pos)
	return func() []*ir.Instr { return []*ir.Instr{find(), data()} }
}

// assert lowers x.(T): with commaOk, its value and ok, the zero value and
// false if it fails; otherwise its value, or a panic with gc's message.
func (l *lowerer) assert(e *ast.TypeAssertExpr, commaOk bool) [][]*ir.Instr {
	pos := l.pos(e, e.Lparen)
	xt, t := l.pkg.Info.Types[e.X].Type, l.pkg.Info.Types[e.Type].Type
	w := l.value(e.X)
	tab, data := l.spill(w[0], pos), l.spill(w[1], pos)
	var res []*ir.Slot
	for _, wt := range l.words(e, t) {
		res = append(res, l.fn.NewSlot("v", wt, pos))
	}
	ok := l.fn.NewSlot("ok", ir.Bool, pos)
	yes, fail, join := l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock()
	bind := l.ifaceTest(xt, t, tab, data, yes, fail, pos)
	l.b = yes
	for i, v := range bind() {
		l.fn.Store(l.b, res[i], v, pos)
	}
	l.fn.Store(l.b, ok, l.fn.BoolConst(l.b, true, pos), pos)
	l.fn.Jump(l.b, join, pos)
	l.b = fail
	if commaOk {
		var zero []*ir.Instr
		if isAggregate(t) {
			m := l.newMem("zero", t, pos)
			l.zero(l.memPlace(m, t, pos), pos)
			zero = []*ir.Instr{l.addr(m, pos)}
		} else {
			for _, s := range res {
				zero = append(zero, l.fn.TypedConst(l.b, s.Type, 0, pos))
			}
		}
		for i, v := range zero {
			l.fn.Store(l.b, res[i], v, pos)
		}
		l.fn.Store(l.b, ok, l.fn.BoolConst(l.b, false, pos), pos)
		l.fn.Jump(l.b, join, pos)
	} else {
		l.assertFail(xt, t, tab, pos)
	}
	l.b = join
	var vals []*ir.Instr
	for _, s := range res {
		vals = append(vals, l.fn.Load(l.b, s, pos))
	}
	if !commaOk {
		return [][]*ir.Instr{vals}
	}
	return [][]*ir.Instr{vals, {l.fn.Load(l.b, ok, pos)}}
}

// samePathFunc returns .samepathN for t: whether a descriptor is of a type
// whose package path is t's, gc's pkgpath (that of a defined type's
// package, or empty).
func (l *lowerer) samePathFunc(t types.Type, pos ir.Pos) *ir.Func {
	path := pkgPath(t)
	if f := l.samePaths[path]; f != nil {
		return f
	}
	f := l.prog.NewFunc(fmt.Sprintf("%ssamepath%d", genPrefix, len(l.samePaths)), l.fn.File, pos)
	f.Params, f.Results = []ir.Type{ir.Ptr}, []ir.Type{ir.Bool}
	if l.samePaths == nil {
		l.samePaths = map[string]*ir.Func{}
	}
	l.samePaths[path] = f
	l.late = append(l.late, func() {
		ps := l.enter(f, pos)
		for _, d := range slices.Clone(l.descs) {
			if pkgPath(d.t) != path {
				continue
			}
			yes, no := l.fn.NewBlock(), l.fn.NewBlock()
			l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, ps[0](), l.addr(d.mem, pos), pos), yes, no, pos)
			l.b = yes
			l.fn.Ret(l.b, pos, l.fn.BoolConst(l.b, true, pos))
			l.b = no
		}
		l.fn.Ret(l.b, pos, l.fn.BoolConst(l.b, false, pos))
		prune(l.fn)
	})
	return f
}

// pkgPath returns the path of the package declaring t, if it is a defined
// type of one, or "".
func pkgPath(t types.Type) string {
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil {
		return n.Obj().Pkg().Path()
	}
	return ""
}

// assertFail ends the current block with gc's panic for a failed x.(t),
// x of static type xt with tab tab.
func (l *lowerer) assertFail(xt, t types.Type, tab func() *ir.Instr, pos ir.Pos) {
	isNil, other := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, tab(), l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos), isNil, other, pos)
	l.b = isNil
	from := gcName(xt)
	if isIface(t) {
		from = "interface"
	}
	l.panicMsg([][]*ir.Instr{l.strWords("interface conversion: "+from+" is nil, not "+gcName(t), pos)}, pos)
	l.b = other
	d := l.dynDesc(xt, tab(), pos)
	if !isIface(t) {
		// When the names are the same, gc tells why the types differ.
		dk := l.keep(d, pos)
		head := func(suffix string) {
			l.panicMsg([][]*ir.Instr{l.strWords("interface conversion: "+gcName(xt)+" is ", pos), l.descName(dk(), pos), l.strWords(", not "+gcName(t)+suffix, pos)}, pos)
		}
		name, want := l.descName(dk(), pos), l.strWords(gcName(t), pos)
		same := l.when(l.fn.Not(l.b, l.fn.Call(l.b, l.streqFunc(pos), []*ir.Instr{name[0], name[1], want[0], want[1]}, pos), pos), pos)
		head("")
		l.b = same
		packages := l.when(l.fn.Call(l.b, l.samePathFunc(t, pos), []*ir.Instr{dk()}, pos), pos)
		head(" (types from different scopes)")
		l.b = packages
		head(" (types from different packages)")
		return
	}
	miss := l.fn.Call(l.b, l.missFunc(t, pos), []*ir.Instr{d, d}, pos)
	missing := []*ir.Instr{l.fn.Extract(l.b, miss, 0, pos), l.fn.Extract(l.b, miss, 1, pos)}
	l.panicMsg([][]*ir.Instr{l.strWords("interface conversion: ", pos), l.descName(d, pos), l.strWords(" is not "+gcName(t)+": missing method ", pos), missing}, pos)
}

// typeSwitch lowers a type switch: its init, then its guard's operand,
// then each case's types in order, the first that matches choosing its
// body, in which the switch's variable, if any, holds the value as that
// type, or as the operand's type for a case of several types, nil, or the
// default.
func (l *lowerer) typeSwitch(s *ast.TypeSwitchStmt) {
	if s.Init != nil {
		l.stmt(s.Init)
	}
	var id *ast.Ident
	var guard ast.Expr
	switch a := s.Assign.(type) {
	case *ast.ExprStmt:
		guard = a.X
	case *ast.AssignStmt:
		id, guard = a.Lhs[0].(*ast.Ident), a.Rhs[0]
	}
	ta, ok := ast.Unparen(guard).(*ast.TypeAssertExpr)
	if !ok {
		l.fail(s, "type switch without a guard")
	}
	pos := l.pos(s, s.Switch)
	xt := l.pkg.Info.Types[ta.X].Type
	w := l.value(ta.X)
	tab, data := l.spill(w[0], pos), l.spill(w[1], pos)
	exit := l.fn.NewBlock()
	bodies := make([]*ir.Block, len(s.Body.List))
	binds := make([]func() []*ir.Instr, len(s.Body.List))
	var dflt *ast.CaseClause
	dfltBody := exit
	for i, c := range s.Body.List {
		cc := c.(*ast.CaseClause)
		bodies[i] = l.fn.NewBlock()
		if cc.List == nil {
			dflt, dfltBody = cc, bodies[i]
			continue
		}
		for _, e := range cc.List {
			epos := l.pos(e, e.Pos())
			next := l.fn.NewBlock()
			if l.pkg.Info.Types[e].IsNil() {
				l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, tab(), l.fn.TypedConst(l.b, ir.Ptr, 0, epos), epos), bodies[i], next, epos)
			} else {
				bind := l.ifaceTest(xt, l.pkg.Info.Types[e].Type, tab, data, bodies[i], next, epos)
				if len(cc.List) == 1 {
					binds[i] = bind
				}
			}
			l.b = next
		}
	}
	l.fn.Jump(l.b, dfltBody, pos)
	_ = dflt
	sw := &target{exit: exit, stmt: s}
	l.targets = append(l.targets, sw)
	done := dflt != nil
	for i, c := range s.Body.List {
		cc := c.(*ast.CaseClause)
		l.b, l.done = bodies[i], false
		if v, ok := l.pkg.Info.Implicits[cc].(*types.Var); ok && id != nil {
			vpos := l.pos(cc, cc.Case)
			var val []*ir.Instr
			if binds[i] != nil {
				val = binds[i]()
			} else {
				val = []*ir.Instr{tab(), data()}
			}
			get := l.spillWords(val, vpos)
			l.store(l.defineObj(id, v), get(), vpos)
		}
		l.stmts(cc.Body)
		if !l.done {
			end := s.Body.Rbrace
			if i+1 < len(s.Body.List) {
				end = s.Body.List[i+1].Pos()
			}
			l.fn.Jump(l.b, exit, l.pos(cc, end))
		}
		done = done && l.done
	}
	l.targets = l.targets[:len(l.targets)-1]
	l.b, l.done = exit, done && !sw.broken
}

// ifaceEqual returns whether the interface values x and y, of type t, are
// equal: the same dynamic type, and nil or equal values of it by its
// equality function, which panics with gc's message if it has none.
func (l *lowerer) ifaceEqual(x, y []*ir.Instr, t types.Type, pos ir.Pos) *ir.Instr {
	gx, gy := l.spillWords(x, pos), l.spillWords(y, pos)
	res := l.fn.NewSlot("eq", ir.Bool, pos)
	same, differ, isNil, nonNil, cmp, uncomp, join := l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, gx()[0], gy()[0], pos), same, differ, pos)
	l.b = differ
	l.fn.Store(l.b, res, l.fn.BoolConst(l.b, false, pos), pos)
	l.fn.Jump(l.b, join, pos)
	l.b = same
	l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, gx()[0], l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos), isNil, nonNil, pos)
	l.b = isNil
	l.fn.Store(l.b, res, l.fn.BoolConst(l.b, true, pos), pos)
	l.fn.Jump(l.b, join, pos)
	l.b = nonNil
	eqf := l.fn.LdM(l.b, ir.Ptr, l.dynDesc(t, gx()[0], pos), l.fn.Const(l.b, 16, pos), pos)
	l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, eqf, l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos), uncomp, cmp, pos)
	l.b = uncomp
	l.panicMsg([][]*ir.Instr{l.strWords("runtime error: comparing uncomparable type ", pos), l.descName(l.dynDesc(t, gx()[0], pos), pos)}, pos)
	l.b = cmp
	d := l.dynDesc(t, gx()[0], pos)
	eqf = l.fn.LdM(l.b, ir.Ptr, d, l.fn.Const(l.b, 16, pos), pos)
	r := l.fn.CallInd(l.b, &ir.Sig{Params: []ir.Type{ir.Ptr, ir.Ptr, ir.Ptr}, Results: []ir.Type{ir.Bool}}, eqf, []*ir.Instr{d, gx()[1], gy()[1]}, pos)
	l.fn.Store(l.b, res, r, pos)
	l.fn.Jump(l.b, join, pos)
	l.b = join
	return l.fn.Load(l.b, res, pos)
}

// ifaceCompare lowers x == y or x != y where x or y is an interface: with
// nil, a test of its tab; otherwise both, left to right, as the one's type
// the other converts to, then compared.
func (l *lowerer) ifaceCompare(e *ast.BinaryExpr, pos ir.Pos) *ir.Instr {
	op := ir.Eq
	if e.Op == token.NEQ {
		op = ir.Ne
	}
	xtv, ytv := l.pkg.Info.Types[e.X], l.pkg.Info.Types[e.Y]
	if xtv.IsNil() || ytv.IsNil() {
		other := e.X
		if xtv.IsNil() {
			other = e.Y
		}
		return l.fn.Compare(l.b, op, l.value(other)[0], l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos)
	}
	t := xtv.Type
	if !isIface(t) || isIface(ytv.Type) && !types.AssignableTo(ytv.Type, t) {
		t = ytv.Type
	}
	xy := l.values([]ast.Expr{e.X, e.Y}, []types.Type{t, t})
	eq := l.ifaceEqual(xy[0], xy[1], t, pos)
	if op == ir.Ne {
		return l.fn.Not(l.b, eq, pos)
	}
	return eq
}
