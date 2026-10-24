// Package lower translates a type-checked S1 program into IR (D-004, D-021).
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
	l := &lowerer{pkg: pkg, slots: map[*types.Var]*ir.Slot{}}
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
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "main" || fd.Recv != nil || fd.Body == nil {
				l.fail(d, "unexpected top-level declaration")
			}
			l.function(prog, fd)
		}
	}
	if len(prog.Funcs) != 1 {
		l.fail(nil, "found %d main functions, want 1", len(prog.Funcs))
	}
	return prog, nil
}

// bailout carries the first lowering failure out of the recursive descent.
type bailout diag.Diagnostic

type lowerer struct {
	pkg   *frontend.Package
	fn    *ir.Func
	b     *ir.Block // the block instructions are appended to
	slots map[*types.Var]*ir.Slot
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

func (l *lowerer) function(prog *ir.Program, fd *ast.FuncDecl) {
	if fd.Type.TypeParams != nil || fd.Type.Params.NumFields() != 0 || fd.Type.Results.NumFields() != 0 {
		l.fail(fd, "unexpected signature for main")
	}
	file := frontend.Position(l.pkg.Fset, fd.Name.Pos()).File
	l.fn = prog.NewFunc("main", file, l.pos(fd.Name, fd.Name.Pos()))
	l.b = l.fn.Entry()
	l.stmts(fd.Body.List)
	// Falling off the end of main's body returns; the closing brace is the
	// return's position, as in gc's line tables.
	l.fn.Ret(l.b, l.pos(fd.Body, fd.Body.Rbrace))
	l.fn = nil
}

func (l *lowerer) stmts(list []ast.Stmt) {
	for _, s := range list {
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
		l.println(s)
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

// println lowers a println call statement: the arguments are evaluated left
// to right, then printed by a single instruction.
func (l *lowerer) println(s *ast.ExprStmt) {
	call, ok := s.X.(*ast.CallExpr)
	if !ok || call.Ellipsis.IsValid() {
		l.fail(s, "unexpected expression statement")
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		l.fail(call, "unexpected call")
	}
	if b, ok := l.pkg.Info.Uses[id].(*types.Builtin); !ok || b.Name() != "println" {
		l.fail(call, "unexpected call of %s", id.Name)
	}
	l.fn.Println(l.b, l.exprs(call.Args), l.pos(call, call.Pos()))
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
		l.fail(id, "%s does not denote a local variable of main", id.Name)
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
