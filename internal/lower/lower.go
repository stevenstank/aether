// Package lower translates a type-checked program of S1 and the functions
// of increment 4.2 into IR (D-004, D-021, D-023).
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

// declare creates the IR function for fd, with its signature: int
// parameters and at most one int result.
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
		if !isInt(v.Type()) {
			l.fail(fd, "parameter of type %s", v.Type())
		}
		f.Params = append(f.Params, ir.I64)
	}
	for v := range sig.Results().Variables() {
		if !isInt(v.Type()) || v.Name() != "" {
			l.fail(fd, "unexpected result %s", v)
		}
		f.Results = append(f.Results, ir.I64)
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
		// that a function with a result cannot fall off its end.
		if len(l.fn.Results) > 0 {
			l.fail(fd.Body, "function %s falls off its end", fd.Name.Name)
		}
		l.fn.Ret(l.b, l.pos(fd.Body, fd.Body.Rbrace))
	}
	l.fn = nil
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
		l.fn.Ret(l.b, l.pos(s, s.Return), l.exprs(s.Results)...)
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

// varSpec lowers "var a, b T = x, y" and "var a, b T". All values are
// evaluated, left to right, before any is stored. A variable without an
// initializer is explicitly stored its zero value, 0, since slots start
// undefined.
func (l *lowerer) varSpec(spec *ast.ValueSpec) {
	if len(spec.Values) == 0 {
		for _, id := range spec.Names {
			slot := l.define(id)
			l.fn.Store(l.b, slot, l.fn.Const(l.b, 0, l.pos(id, id.Pos())), l.pos(id, id.Pos()))
		}
		return
	}
	if len(spec.Values) != len(spec.Names) {
		l.fail(spec, "%d names but %d values", len(spec.Names), len(spec.Values))
	}
	vals := l.exprs(spec.Values)
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
		vals := l.exprs(s.Rhs)
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
		x := l.fn.Load(l.b, slot, l.pos(id, id.Pos()))
		y := l.expr(s.Rhs[0])
		l.fn.Store(l.b, slot, l.fn.Binary(l.b, op, x, y, l.pos(s, s.TokPos)), l.pos(id, id.Pos()))
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
			l.fn.Println(l.b, l.exprs(call.Args), l.pos(call, call.Pos()))
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
	return l.fn.Call(l.b, l.funcs[obj], l.exprs(call.Args), l.pos(call, call.Pos()))
}

// ident returns e as the identifier of an assignment target.
func (l *lowerer) ident(e ast.Expr) *ast.Ident {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" {
		l.fail(e, "unexpected assignment target %s", types.ExprString(e))
	}
	return id
}

// define creates the slot for the int variable that id declares.
func (l *lowerer) define(id *ast.Ident) *ir.Slot {
	v, ok := l.pkg.Info.Defs[id].(*types.Var)
	if !ok || !isInt(v.Type()) {
		l.fail(id, "%s does not declare an int variable", id.Name)
	}
	if l.slots[v] != nil {
		l.fail(id, "variable %s declared twice", id.Name)
	}
	slot := l.fn.NewSlot(id.Name, ir.I64, l.pos(id, id.Pos()))
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

func (l *lowerer) exprs(list []ast.Expr) []*ir.Instr {
	vals := make([]*ir.Instr, len(list))
	for i, e := range list {
		vals[i] = l.expr(e)
	}
	return vals
}

// expr lowers an int expression and returns its value. Operands are
// evaluated left to right.
func (l *lowerer) expr(e ast.Expr) *ir.Instr {
	tv, ok := l.pkg.Info.Types[e]
	if !ok || !isInt(tv.Type) && !types.Identical(tv.Type, types.Typ[types.UntypedInt]) {
		l.fail(e, "expression %s is not of type int", types.ExprString(e))
	}
	if tv.Value != nil {
		// A constant expression, of any form: use the exact value go/types
		// computed, without evaluating it again.
		if tv.Value.Kind() != constant.Int {
			l.fail(e, "constant %s is not an integer", tv.Value)
		}
		n, exact := constant.Int64Val(tv.Value)
		if !exact {
			l.fail(e, "constant %s does not fit in 64 bits", tv.Value)
		}
		return l.fn.Const(l.b, n, l.pos(e, e.Pos()))
	}
	switch e := e.(type) {
	case *ast.Ident:
		return l.fn.Load(l.b, l.lookup(e), l.pos(e, e.Pos()))
	case *ast.ParenExpr:
		return l.expr(e.X)
	case *ast.CallExpr:
		v := l.call(e)
		if v.Type != ir.I64 {
			l.fail(e, "call of a function without a result used as a value")
		}
		return v
	case *ast.UnaryExpr:
		switch e.Op {
		case token.ADD:
			return l.expr(e.X)
		case token.SUB:
			return l.fn.Neg(l.b, l.expr(e.X), l.pos(e, e.OpPos))
		}
	case *ast.BinaryExpr:
		op, ok := map[token.Token]ir.Op{token.ADD: ir.Add, token.SUB: ir.Sub, token.MUL: ir.Mul}[e.Op]
		if ok {
			x := l.expr(e.X)
			y := l.expr(e.Y)
			return l.fn.Binary(l.b, op, x, y, l.pos(e, e.OpPos))
		}
	}
	l.fail(e, "unexpected expression %s", types.ExprString(e))
	return nil
}

func isInt(t types.Type) bool { return types.Identical(t, types.Typ[types.Int]) }
