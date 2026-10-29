// Package lower translates a type-checked program of S1, the functions of
// increment 4.2, and the bool and control flow of increment 4.1 into IR
// (D-004, D-021, D-023).
//
// IR values are local to their block (D-023). && and || branch, so a value
// computed before an operand containing them is kept in a temporary slot
// across the branch. Code after a return, break, or continue is not
// lowered, and blocks that no path reaches are removed.
//
// Lowering runs only on programs the subset gate has accepted, so every
// construct it meets should be in S1. It nevertheless checks each assumption
// it relies on and reports a violation as an Internal diagnostic: an
// unexpected construct here is a bug in the gate or in lowering, never a
// problem with the user's program. Lowering never emits IR for a construct
// it does not recognize.
//
// Lowering does not evaluate constant expressions. go/types has already
// computed their exact values (D-012); lowering emits each as an I64
// constant and fails if the exact value is not representable. Machine
// arithmetic is never used on constants.
package lower

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/ir"
)

// Lower translates pkg, which must have passed the subset gate, into IR. It
// returns either a program or Internal diagnostics, never both. It does not
// verify the program; the caller runs ir.Verify.
func Lower(pkg *frontend.Package) (prog *ir.Program, ds []diag.Diagnostic) {
	l := &lowerer{pkg: pkg, slots: map[*types.Var]*ir.Slot{}, funcs: map[*types.Func]*ir.Func{}}
	defer func() {
		if r := recover(); r != nil {
			b, ok := r.(bailout)
			if !ok {
				panic(r)
			}
			prog, ds = nil, []diag.Diagnostic{diag.Diagnostic(b)}
		}
	}()
	prog = &ir.Program{}
	// Every function is created before any body is lowered, so that a call
	// can refer to a function declared later.
	var decls []*ast.FuncDecl
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil || fd.Type.TypeParams != nil {
				l.fail(d, "unexpected top-level declaration")
			}
			l.declare(prog, fd)
			decls = append(decls, fd)
		}
	}
	for _, fd := range decls {
		l.function(fd)
	}
	mains := 0
	for _, f := range prog.Funcs {
		if f.Name == "main" {
			mains++
		}
	}
	if mains != 1 {
		l.fail(nil, "found %d main functions, want 1", mains)
	}
	return prog, nil
}

// bailout carries the first lowering failure out of the recursive descent.
type bailout diag.Diagnostic

type lowerer struct {
	pkg   *frontend.Package
	fn    *ir.Func
	b     *ir.Block // the block instructions are appended to
	done  bool      // the block has its terminator: the rest is unreachable
	slots map[*types.Var]*ir.Slot
	funcs map[*types.Func]*ir.Func
	loops []*loop // the enclosing for statements, innermost last
}

// loop is a for statement being lowered: break jumps to exit, continue to
// next (the post statement, or the condition).
type loop struct {
	next, exit *ir.Block
	broken     bool // a break targets exit
}

func (l *lowerer) fail(n ast.Node, format string, args ...any) {
	d := diag.Diagnostic{Class: diag.Internal, Message: "lowering: " + fmt.Sprintf(format, args...)}
	if n != nil {
		d.Pos = frontend.Position(l.pkg.Fset, n.Pos())
	}
	panic(bailout(d))
}

// pos converts p to an IR position. Every position in a function must lie
// in the function's own file.
func (l *lowerer) pos(n ast.Node, p token.Pos) ir.Pos {
	dp := frontend.Position(l.pkg.Fset, p)
	if dp.Line <= 0 || dp.Col <= 0 || l.fn != nil && dp.File != l.fn.File {
		l.fail(n, "invalid source position %v", dp)
	}
	return ir.Pos{Line: dp.Line, Col: dp.Col}
}

// declare creates the IR function for fd, with its signature: int or bool
// parameters and at most one int or bool result.
func (l *lowerer) declare(prog *ir.Program, fd *ast.FuncDecl) {
	obj, ok := l.pkg.Info.Defs[fd.Name].(*types.Func)
	if !ok {
		l.fail(fd, "no function object for %s", fd.Name.Name)
	}
	sig := obj.Type().(*types.Signature)
	if sig.Variadic() || sig.Results().Len() > 1 {
		l.fail(fd, "unexpected signature for %s", fd.Name.Name)
	}
	file := frontend.Position(l.pkg.Fset, fd.Name.Pos()).File
	f := prog.NewFunc(fd.Name.Name, file, l.pos(fd.Name, fd.Name.Pos()))
	for v := range sig.Params().Variables() {
		f.Params = append(f.Params, l.irType(fd, v.Type()))
	}
	for v := range sig.Results().Variables() {
		if v.Name() != "" {
			l.fail(fd, "unexpected result %s", v)
		}
		f.Results = append(f.Results, l.irType(fd, v.Type()))
	}
	l.funcs[obj] = f
}

// function lowers fd's body. Each named parameter gets a slot, stored from
// its param at entry; blank and unnamed parameters are never read.
func (l *lowerer) function(fd *ast.FuncDecl) {
	l.fn = l.funcs[l.pkg.Info.Defs[fd.Name].(*types.Func)]
	l.b = l.fn.Entry()
	l.done = false
	i := 0
	for _, field := range fd.Type.Params.List {
		if len(field.Names) == 0 {
			i++
		}
		for _, id := range field.Names {
			if id.Name != "_" {
				p := l.fn.Param(l.b, i, l.pos(id, id.Pos()))
				l.fn.Store(l.b, l.define(id), p, l.pos(id, id.Pos()))
			}
			i++
		}
	}
	l.stmts(fd.Body.List)
	if !l.done {
		// Falling off the end of a body returns; the closing brace is the
		// return's position, as in gc's line tables. go/types has checked
		// that a function with a result cannot fall off its end, and
		// lowering marks the end of "for {}" without break unreachable.
		if len(l.fn.Results) > 0 {
			l.fail(fd.Body, "function %s falls off its end", fd.Name.Name)
		}
		l.fn.Ret(l.b, l.pos(fd.Body, fd.Body.Rbrace))
	}
	prune(l.fn)
	l.fn = nil
}

// prune removes the blocks that no path from the entry reaches and
// renumbers the others, so that block IDs equal indices (D-023 invariant 1).
func prune(f *ir.Func) {
	seen := map[*ir.Block]bool{f.Entry(): true}
	work := []*ir.Block{f.Entry()}
	for len(work) > 0 {
		b := work[len(work)-1]
		work = work[:len(work)-1]
		if len(b.Instrs) == 0 {
			continue // reported by the verifier
		}
		for _, t := range b.Instrs[len(b.Instrs)-1].Targets {
			if !seen[t] {
				seen[t] = true
				work = append(work, t)
			}
		}
	}
	var kept []*ir.Block
	for _, b := range f.Blocks {
		if seen[b] {
			b.ID = len(kept)
			kept = append(kept, b)
		}
	}
	f.Blocks = kept
}

// stmts lowers a statement list up to its first return. What follows a
// return is unreachable and is not lowered (D-023 invariant 1).
func (l *lowerer) stmts(list []ast.Stmt) {
	for _, s := range list {
		if l.done {
			return
		}
		l.stmt(s)
	}
}

func (l *lowerer) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.DeclStmt:
		d, ok := s.Decl.(*ast.GenDecl)
		if !ok {
			l.fail(s, "unexpected declaration %T", s.Decl)
		}
		switch d.Tok {
		case token.VAR:
			for _, spec := range d.Specs {
				l.varSpec(spec.(*ast.ValueSpec))
			}
		case token.CONST:
			// Constants have no storage. Each use is an expression whose
			// exact value go/types recorded, lowered by expr.
		default:
			l.fail(d, "unexpected %s declaration", d.Tok)
		}
	case *ast.AssignStmt:
		l.assign(s)
	case *ast.IncDecStmt:
		op := ir.Add
		if s.Tok == token.DEC {
			op = ir.Sub
		}
		id := l.ident(s.X)
		slot := l.lookup(id)
		x := l.fn.Load(l.b, slot, l.pos(id, id.Pos()))
		one := l.fn.Const(l.b, 1, l.pos(s, s.TokPos))
		l.fn.Store(l.b, slot, l.fn.Binary(l.b, op, x, one, l.pos(s, s.TokPos)), l.pos(id, id.Pos()))
	case *ast.ExprStmt:
		l.exprStmt(s)
	case *ast.ReturnStmt:
		// go/types has checked the results against the signature.
		l.fn.Ret(l.b, l.pos(s, s.Return), l.values(s.Results)...)
		l.done = true
	case *ast.IfStmt:
		l.ifStmt(s)
	case *ast.ForStmt:
		l.forStmt(s)
	case *ast.BranchStmt:
		if s.Label != nil || len(l.loops) == 0 || s.Tok != token.BREAK && s.Tok != token.CONTINUE {
			l.fail(s, "unexpected %s statement", s.Tok)
		}
		lp := l.loops[len(l.loops)-1]
		target := lp.next
		if s.Tok == token.BREAK {
			target, lp.broken = lp.exit, true
		}
		l.fn.Jump(l.b, target, l.pos(s, s.Pos()))
		l.done = true
	case *ast.BlockStmt:
		// A block only scopes names, which go/types has resolved: each
		// declaration already denotes its own object and gets its own slot.
		l.stmts(s.List)
	case *ast.EmptyStmt:
	default:
		l.fail(s, "unexpected statement %T", s)
	}
}

// ifStmt lowers "if init; cond { … } else …". The branch is at the if
// keyword, and the jump that ends each arm at its closing brace. The
// statement ends unreachably if both arms do.
func (l *lowerer) ifStmt(s *ast.IfStmt) {
	if s.Init != nil {
		l.stmt(s.Init)
	}
	cond := l.expr(s.Cond)
	then, join := l.fn.NewBlock(), l.fn.NewBlock()
	els := join
	if s.Else != nil {
		els = l.fn.NewBlock()
	}
	l.fn.Branch(l.b, cond, then, els, l.pos(s, s.If))
	l.b, l.done = then, false
	l.stmts(s.Body.List)
	if !l.done {
		l.fn.Jump(l.b, join, l.pos(s.Body, s.Body.Rbrace))
	}
	done := l.done
	if s.Else != nil {
		l.b, l.done = els, false
		l.stmt(s.Else)
		if !l.done {
			l.fn.Jump(l.b, join, l.pos(s.Else, s.Else.End()-1))
		}
		done = done && l.done
	} else {
		done = false
	}
	l.b, l.done = join, done
}

// forStmt lowers the three for forms. The condition is evaluated in its
// own block, which the entry and the end of each iteration jump to; the
// post statement, if any, has its own block, where continue goes. Go 1.22
// gives each iteration its own copy of a variable declared in init, but
// without closures or pointers no program can observe the difference, so
// one slot serves all iterations.
func (l *lowerer) forStmt(s *ast.ForStmt) {
	if s.Init != nil {
		l.stmt(s.Init)
	}
	head, body, exit := l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock()
	next := head
	if s.Post != nil {
		next = l.fn.NewBlock()
	}
	pos := l.pos(s, s.For)
	l.fn.Jump(l.b, head, pos)
	l.b = head
	if s.Cond != nil {
		l.fn.Branch(l.b, l.expr(s.Cond), body, exit, pos)
	} else {
		l.fn.Jump(l.b, body, pos)
	}
	lp := &loop{next: next, exit: exit}
	l.loops = append(l.loops, lp)
	l.b, l.done = body, false
	l.stmts(s.Body.List)
	if !l.done {
		l.fn.Jump(l.b, next, l.pos(s.Body, s.Body.Rbrace))
	}
	l.loops = l.loops[:len(l.loops)-1]
	if s.Post != nil {
		l.b, l.done = next, false
		l.stmt(s.Post)
		l.fn.Jump(l.b, head, l.pos(s.Post, s.Post.Pos()))
	}
	// Without a condition, only a break reaches the exit.
	l.b, l.done = exit, s.Cond == nil && !lp.broken
}

// varSpec lowers "var a, b T = x, y" and "var a, b T". All values are
// evaluated, left to right, before any is stored. A variable without an
// initializer is explicitly stored its zero value, 0 or false, since slots
// start undefined.
func (l *lowerer) varSpec(spec *ast.ValueSpec) {
	if len(spec.Values) == 0 {
		for _, id := range spec.Names {
			slot := l.define(id)
			pos := l.pos(id, id.Pos())
			var zero *ir.Instr
			if slot.Type == ir.Bool {
				zero = l.fn.BoolConst(l.b, false, pos)
			} else {
				zero = l.fn.Const(l.b, 0, pos)
			}
			l.fn.Store(l.b, slot, zero, pos)
		}
		return
	}
	if len(spec.Values) != len(spec.Names) {
		l.fail(spec, "%d names but %d values", len(spec.Names), len(spec.Values))
	}
	vals := l.values(spec.Values)
	for i, id := range spec.Names {
		l.fn.Store(l.b, l.define(id), vals[i], l.pos(id, id.Pos()))
	}
}

// assign lowers =, :=, and the compound assignments. For = and :=, every
// right-hand side is evaluated, left to right, before the stores, which then
// happen left to right (spec §Assignment statements), so "a, b = b, a"
// swaps. In "a, c := 3, 4" with a already declared in the same scope, a is
// in Uses rather than Defs and is assigned, not redeclared.
func (l *lowerer) assign(s *ast.AssignStmt) {
	switch s.Tok {
	case token.ASSIGN, token.DEFINE:
		if len(s.Lhs) != len(s.Rhs) {
			l.fail(s, "%d targets but %d values", len(s.Lhs), len(s.Rhs))
		}
		vals := l.values(s.Rhs)
		for i, e := range s.Lhs {
			id := l.ident(e)
			var slot *ir.Slot
			if s.Tok == token.DEFINE && l.pkg.Info.Defs[id] != nil {
				slot = l.define(id)
			} else {
				slot = l.lookup(id)
			}
			l.fn.Store(l.b, slot, vals[i], l.pos(id, id.Pos()))
		}
	case token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN:
		if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
			l.fail(s, "compound assignment with %d targets and %d values", len(s.Lhs), len(s.Rhs))
		}
		op := map[token.Token]ir.Op{token.ADD_ASSIGN: ir.Add, token.SUB_ASSIGN: ir.Sub, token.MUL_ASSIGN: ir.Mul}[s.Tok]
		id := l.ident(s.Lhs[0])
		slot := l.lookup(id)
		// The right side may branch (a call argument with && or ||), so x
		// is kept across it.
		pos := l.pos(id, id.Pos())
		x := l.fn.Load(l.b, slot, pos)
		get := func() *ir.Instr { return x }
		if l.branches(s.Rhs) {
			get = l.spill(x, pos)
		}
		y := l.expr(s.Rhs[0])
		l.fn.Store(l.b, slot, l.fn.Binary(l.b, op, get(), y, l.pos(s, s.TokPos)), pos)
	default:
		l.fail(s, "unexpected assignment %s", s.Tok)
	}
}

// exprStmt lowers a call statement. For println, the arguments are
// evaluated left to right, then printed by a single instruction. A call of
// a function discards its result, if any.
func (l *lowerer) exprStmt(s *ast.ExprStmt) {
	call, ok := s.X.(*ast.CallExpr)
	if !ok || call.Ellipsis.IsValid() {
		l.fail(s, "unexpected expression statement")
	}
	if id, ok := call.Fun.(*ast.Ident); ok {
		if b, ok := l.pkg.Info.Uses[id].(*types.Builtin); ok && b.Name() == "println" {
			l.fn.Println(l.b, l.values(call.Args), l.pos(call, call.Pos()))
			return
		}
	}
	l.call(call)
}

// call lowers a direct call of a function of the program: the arguments are
// evaluated left to right, then the function is called.
func (l *lowerer) call(call *ast.CallExpr) *ir.Instr {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || call.Ellipsis.IsValid() {
		l.fail(call, "unexpected call")
	}
	obj, ok := l.pkg.Info.Uses[id].(*types.Func)
	if !ok || l.funcs[obj] == nil {
		l.fail(call, "unexpected call of %s", id.Name)
	}
	return l.fn.Call(l.b, l.funcs[obj], l.values(call.Args), l.pos(call, call.Pos()))
}

// ident returns e as the identifier of an assignment target.
func (l *lowerer) ident(e ast.Expr) *ast.Ident {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" {
		l.fail(e, "unexpected assignment target %s", types.ExprString(e))
	}
	return id
}

// define creates the slot for the int or bool variable that id declares.
func (l *lowerer) define(id *ast.Ident) *ir.Slot {
	v, ok := l.pkg.Info.Defs[id].(*types.Var)
	if !ok {
		l.fail(id, "%s does not declare a variable", id.Name)
	}
	if l.slots[v] != nil {
		l.fail(id, "variable %s declared twice", id.Name)
	}
	slot := l.fn.NewSlot(id.Name, l.irType(id, v.Type()), l.pos(id, id.Pos()))
	l.slots[v] = slot
	return slot
}

// lookup returns the slot of the variable that id uses.
func (l *lowerer) lookup(id *ast.Ident) *ir.Slot {
	v, ok := l.pkg.Info.Uses[id].(*types.Var)
	if !ok || l.slots[v] == nil {
		l.fail(id, "%s does not denote a local variable", id.Name)
	}
	return l.slots[v]
}

// values lowers list left to right. A value computed before a later
// operand that branches is stored in a temporary slot and loaded again
// after the last operand, in the block where the values are used.
func (l *lowerer) values(list []ast.Expr) []*ir.Instr {
	// later[i] reports whether an operand after list[i] branches. It is
	// computed in one pass, since println may have very many operands.
	later := make([]bool, len(list))
	for i := len(list) - 2; i >= 0; i-- {
		later[i] = later[i+1] || l.branches(list[i+1:i+2])
	}
	get := make([]func() *ir.Instr, len(list))
	for i, e := range list {
		v, pos := l.expr(e), l.pos(e, e.Pos())
		get[i] = func() *ir.Instr { return v }
		if later[i] {
			get[i] = l.spill(v, pos)
		}
	}
	vals := make([]*ir.Instr, len(list))
	for i := range get {
		vals[i] = get[i]()
	}
	return vals
}

// spill stores v in a temporary slot and returns a function that loads it
// again in the then current block.
func (l *lowerer) spill(v *ir.Instr, pos ir.Pos) func() *ir.Instr {
	tmp := l.fn.NewSlot("tmp", v.Type, pos)
	l.fn.Store(l.b, tmp, v, pos)
	return func() *ir.Instr { return l.fn.Load(l.b, tmp, pos) }
}

// branches reports whether lowering any of list would branch: whether it
// contains a non-constant && or ||.
func (l *lowerer) branches(list []ast.Expr) bool {
	found := false
	for _, e := range list {
		ast.Inspect(e, func(n ast.Node) bool {
			if e, ok := n.(ast.Expr); ok {
				if tv, ok := l.pkg.Info.Types[e]; ok && tv.Value != nil {
					return false // a constant: lowered as one const
				}
			}
			if b, ok := n.(*ast.BinaryExpr); ok && (b.Op == token.LAND || b.Op == token.LOR) {
				found = true
			}
			return !found
		})
	}
	return found
}

// expr lowers an int or bool expression and returns its value. Operands
// are evaluated left to right.
func (l *lowerer) expr(e ast.Expr) *ir.Instr {
	tv, ok := l.pkg.Info.Types[e]
	if !ok {
		l.fail(e, "no type for expression %s", types.ExprString(e))
	}
	t := l.irType(e, tv.Type)
	if tv.Value != nil {
		// A constant expression, of any form: use the exact value go/types
		// computed, without evaluating it again.
		pos := l.pos(e, e.Pos())
		if t == ir.Bool {
			return l.fn.BoolConst(l.b, constant.BoolVal(tv.Value), pos)
		}
		n, exact := constant.Int64Val(tv.Value)
		if tv.Value.Kind() != constant.Int || !exact {
			l.fail(e, "constant %s does not fit in 64 bits", tv.Value)
		}
		return l.fn.Const(l.b, n, pos)
	}
	switch e := e.(type) {
	case *ast.Ident:
		return l.fn.Load(l.b, l.lookup(e), l.pos(e, e.Pos()))
	case *ast.ParenExpr:
		return l.expr(e.X)
	case *ast.CallExpr:
		v := l.call(e)
		if v.Type != t {
			l.fail(e, "call of type %s used as a value of type %s", v.Type, t)
		}
		return v
	case *ast.UnaryExpr:
		switch e.Op {
		case token.ADD:
			return l.expr(e.X)
		case token.SUB:
			return l.fn.Neg(l.b, l.expr(e.X), l.pos(e, e.OpPos))
		case token.NOT:
			return l.fn.Not(l.b, l.expr(e.X), l.pos(e, e.OpPos))
		}
	case *ast.BinaryExpr:
		pos := l.pos(e, e.OpPos)
		switch e.Op {
		case token.LAND, token.LOR:
			return l.logical(e, pos)
		}
		if op, ok := binaryOps[e.Op]; ok {
			xy := l.values([]ast.Expr{e.X, e.Y})
			if op.IsComparison() {
				return l.fn.Compare(l.b, op, xy[0], xy[1], pos)
			}
			return l.fn.Binary(l.b, op, xy[0], xy[1], pos)
		}
	}
	l.fail(e, "unexpected expression %s", types.ExprString(e))
	return nil
}

var binaryOps = map[token.Token]ir.Op{
	token.ADD: ir.Add, token.SUB: ir.Sub, token.MUL: ir.Mul,
	token.EQL: ir.Eq, token.NEQ: ir.Ne, token.LSS: ir.Lt, token.LEQ: ir.Le, token.GTR: ir.Gt, token.GEQ: ir.Ge,
}

// logical lowers x && y and x || y: x is stored in a temporary slot, and
// y is evaluated, and stored over it, only if x does not decide the
// result. Both branches are at the operator.
func (l *lowerer) logical(e *ast.BinaryExpr, pos ir.Pos) *ir.Instr {
	tmp := l.fn.NewSlot("tmp", ir.Bool, pos)
	x := l.expr(e.X)
	l.fn.Store(l.b, tmp, x, pos)
	right, join := l.fn.NewBlock(), l.fn.NewBlock()
	if e.Op == token.LAND {
		l.fn.Branch(l.b, x, right, join, pos)
	} else {
		l.fn.Branch(l.b, x, join, right, pos)
	}
	l.b = right
	l.fn.Store(l.b, tmp, l.expr(e.Y), pos)
	l.fn.Jump(l.b, join, pos)
	l.b = join
	return l.fn.Load(l.b, tmp, pos)
}

// irType returns the IR type of an int or bool Go type, typed or untyped.
func (l *lowerer) irType(n ast.Node, t types.Type) ir.Type {
	switch {
	case types.Identical(t, types.Typ[types.Int]) || types.Identical(t, types.Typ[types.UntypedInt]):
		return ir.I64
	case types.Identical(t, types.Typ[types.Bool]) || types.Identical(t, types.Typ[types.UntypedBool]):
		return ir.Bool
	}
	l.fail(n, "unexpected type %s", t)
	return ir.NoType
}
