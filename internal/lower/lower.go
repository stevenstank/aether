// Package lower translates a type-checked program of S1, the functions of
// increment 4.2, the bool and control flow of increment 4.1, the multiple
// results of increment 4.3, the integer types of increment 4.4, the
// division and shifts of increment 4.5, the package-level variables and
// constants of increment 4.6, the switch statements of increment 4.7, and
// the float64 and float32 values of Phase 6 increment 6.1, and the string
// constants of increment 6.2 into IR (D-004, D-021, D-023, D-032, D-033).
//
// Each package-level variable is an IR global. Their initializers are
// lowered, in types.Info.InitOrder, into the program's Init, which runs
// before main; a program without initializers has no Init.
//
// IR values are local to their block (D-023). && and || branch, and so do
// the checks before a division and a signed shift (D-023 explicit checks),
// so a value computed before an operand containing them is kept in a
// temporary slot across the branch. Code after a return, break, or continue is not
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
// computed their exact values (D-012); lowering emits each as a constant of
// its IR type and fails if the exact value is not representable. Machine
// arithmetic is never used on constants.
package lower

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/ir"
)

// Lower translates pkg, which must have passed the subset gate, into IR. It
// returns either a program or Internal diagnostics, never both. It does not
// verify the program; the caller runs ir.Verify.
func Lower(pkg *frontend.Package) (prog *ir.Program, ds []diag.Diagnostic) {
	l := &lowerer{pkg: pkg, slots: map[*types.Var][]*ir.Slot{}, globals: map[*types.Var][]*ir.Global{}, funcs: map[*types.Func]*ir.Func{}}
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
	l.prog = prog
	// Every function and global is created before any body is lowered, so
	// that a use can refer to one declared later.
	var decls []*ast.FuncDecl
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok {
				l.global(prog, gd)
				continue
			}
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
	l.init(prog)
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

// A Go value is one IR value, its word, or for a string two: the address of
// its bytes (a Ptr, 0 for the empty string) and its I64 length (D-033).
// Variables, parameters, and results of type string are two slots, globals,
// parameters, or results, and every Go value is lowered as the list of its
// words.
type lowerer struct {
	pkg     *frontend.Package
	prog    *ir.Program
	fn      *ir.Func
	sig     *types.Signature // fn's Go signature; nil for Init
	b       *ir.Block        // the block instructions are appended to
	done    bool             // the block has its terminator: the rest is unreachable
	slots   map[*types.Var][]*ir.Slot
	globals map[*types.Var][]*ir.Global
	funcs   map[*types.Func]*ir.Func
	targets []*target // the enclosing for and switch statements, innermost last
}

// target is a for or switch statement being lowered: break jumps to exit,
// and continue, in a for statement, to next (the post statement, or the
// condition). A switch has no next.
type target struct {
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
// parameters and unnamed results.
func (l *lowerer) declare(prog *ir.Program, fd *ast.FuncDecl) {
	obj, ok := l.pkg.Info.Defs[fd.Name].(*types.Func)
	if !ok {
		l.fail(fd, "no function object for %s", fd.Name.Name)
	}
	sig := obj.Type().(*types.Signature)
	if sig.Variadic() {
		l.fail(fd, "unexpected signature for %s", fd.Name.Name)
	}
	file := frontend.Position(l.pkg.Fset, fd.Name.Pos()).File
	f := prog.NewFunc(fd.Name.Name, file, l.pos(fd.Name, fd.Name.Pos()))
	for v := range sig.Params().Variables() {
		f.Params = append(f.Params, l.words(fd, v.Type())...)
	}
	for v := range sig.Results().Variables() {
		if v.Name() != "" {
			l.fail(fd, "unexpected result %s", v)
		}
		f.Results = append(f.Results, l.words(fd, v.Type())...)
	}
	l.funcs[obj] = f
}

// global creates the IR globals of a package-level var declaration, in
// source order. A package-level constant, like a local one, has no storage.
func (l *lowerer) global(prog *ir.Program, d *ast.GenDecl) {
	switch d.Tok {
	case token.CONST:
		return
	case token.VAR:
	default:
		l.fail(d, "unexpected package-level %s declaration", d.Tok)
	}
	for _, spec := range d.Specs {
		for _, id := range spec.(*ast.ValueSpec).Names {
			v, ok := l.pkg.Info.Defs[id].(*types.Var)
			if !ok || id.Name == "_" {
				l.fail(id, "%s does not declare a variable", id.Name)
			}
			file := frontend.Position(l.pkg.Fset, id.Pos()).File
			// A string's words are the globals name.ptr and name.len, names
			// no Go identifier has.
			ts := l.words(id, v.Type())
			for i, t := range ts {
				name := id.Name
				if len(ts) == 2 {
					name += [2]string{".ptr", ".len"}[i]
				}
				l.globals[v] = append(l.globals[v], prog.NewGlobal(name, t, file, l.pos(id, id.Pos())))
			}
		}
	}
}

// init lowers the package-level variables' initializers into the program's
// Init, in types.Info.InitOrder (spec §Package initialization): each
// evaluates its expression, which may call functions, then stores the
// value, or each result of a call, in its variables. A variable without an
// initializer keeps its zero value. Init is at the package clause.
func (l *lowerer) init(prog *ir.Program) {
	order := l.pkg.Info.InitOrder
	if len(order) == 0 {
		return
	}
	f := l.pkg.Files[0]
	file := frontend.Position(l.pkg.Fset, f.Package).File
	l.fn = prog.NewFunc("init", file, l.pos(f, f.Package))
	prog.Init = l.fn
	l.b, l.done = l.fn.Entry(), false
	for _, init := range order {
		vals := l.valuesOrResults([]ast.Expr{init.Rhs}, len(init.Lhs))
		for i, v := range init.Lhs {
			if l.globals[v] == nil {
				l.fail(init.Rhs, "initializer of %s, which is not a global", v.Name())
			}
			l.store(variable{globals: l.globals[v]}, vals[i], l.pos(init.Rhs, v.Pos()))
		}
	}
	l.fn.Ret(l.b, l.fn.Pos)
	prune(l.fn)
	l.fn = nil
}

// function lowers fd's body. Each named parameter gets a slot, stored from
// its param at entry; blank and unnamed parameters are never read.
func (l *lowerer) function(fd *ast.FuncDecl) {
	obj := l.pkg.Info.Defs[fd.Name].(*types.Func)
	l.fn, l.sig = l.funcs[obj], obj.Type().(*types.Signature)
	l.b = l.fn.Entry()
	l.done = false
	i := 0 // the first IR parameter of the next Go one
	for _, field := range fd.Type.Params.List {
		n := len(l.words(field.Type, l.pkg.Info.Types[field.Type].Type))
		if len(field.Names) == 0 {
			i += n
		}
		for _, id := range field.Names {
			if id.Name != "_" {
				pos := l.pos(id, id.Pos())
				for j, slot := range l.define(id) {
					l.fn.Store(l.b, slot, l.fn.Param(l.b, i+j, pos), pos)
				}
			}
			i += n
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
	l.fn, l.sig = nil, nil
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
		v := l.lookup(id)
		x := l.scalar(id, l.load(v, l.pos(id, id.Pos())))
		pos := l.pos(s, s.TokPos)
		if x.Type.IsFloat() {
			// x++ adds the constant 1 of x's type (spec §IncDec statements).
			one := l.fn.FloatConst(l.b, x.Type, 1, pos)
			l.store(v, []*ir.Instr{l.arith(op, x, one, nil, pos)}, l.pos(id, id.Pos()))
			return
		}
		one := l.fn.TypedConst(l.b, x.Type, 1, pos)
		l.store(v, []*ir.Instr{l.fn.Binary(l.b, op, x, one, pos)}, l.pos(id, id.Pos()))
	case *ast.ExprStmt:
		l.exprStmt(s)
	case *ast.ReturnStmt:
		// go/types has checked the results against the signature.
		l.fn.Ret(l.b, l.pos(s, s.Return), flatten(l.valuesOrResults(s.Results, l.sig.Results().Len()))...)
		l.done = true
	case *ast.IfStmt:
		l.ifStmt(s)
	case *ast.ForStmt:
		l.forStmt(s)
	case *ast.SwitchStmt:
		l.switchStmt(s)
	case *ast.BranchStmt:
		if s.Label != nil || s.Tok != token.BREAK && s.Tok != token.CONTINUE {
			l.fail(s, "unexpected %s statement", s.Tok)
		}
		// break leaves the innermost for or switch, continue the innermost
		// for.
		var dest *ir.Block
		for i := len(l.targets) - 1; i >= 0 && dest == nil; i-- {
			t := l.targets[i]
			if s.Tok == token.BREAK {
				dest, t.broken = t.exit, true
			} else {
				dest = t.next
			}
		}
		if dest == nil {
			l.fail(s, "%s outside a loop", s.Tok)
		}
		l.fn.Jump(l.b, dest, l.pos(s, s.Pos()))
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
	lp := &target{next: next, exit: exit}
	l.targets = append(l.targets, lp)
	l.b, l.done = body, false
	l.stmts(s.Body.List)
	if !l.done {
		l.fn.Jump(l.b, next, l.pos(s.Body, s.Body.Rbrace))
	}
	l.targets = l.targets[:len(l.targets)-1]
	if s.Post != nil {
		l.b, l.done = next, false
		l.stmt(s.Post)
		l.fn.Jump(l.b, head, l.pos(s.Post, s.Post.Pos()))
	}
	// Without a condition, only a break reaches the exit.
	l.b, l.done = exit, s.Cond == nil && !lp.broken
}

// switchStmt lowers "switch init; tag { case x, y: … default: … }" as a
// chain of tests. The tag is evaluated once, stored in a temporary slot,
// and compared for equality with each case value in source order, left to
// right and top to bottom; each case value is evaluated only if no earlier
// one matched (Go spec, Switch statements). Without a tag, each case value
// is itself the condition. If none matches, the default clause runs, wherever
// it appears, or the statement ends. go/types has converted every case value
// to the tag's type.
//
// The tests are at their case values, and the jump that ends each clause at
// the next clause's case keyword, or the closing brace. The statement ends unreachably if it has a default
// clause, every clause ends unreachably, and no break leaves it.
func (l *lowerer) switchStmt(s *ast.SwitchStmt) {
	if s.Init != nil {
		l.stmt(s.Init)
	}
	var tag func() *ir.Instr
	if s.Tag != nil {
		tag = l.spill(l.expr(s.Tag), l.pos(s.Tag, s.Tag.Pos()))
	}
	exit := l.fn.NewBlock()
	bodies := make([]*ir.Block, len(s.Body.List))
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
			pos := l.pos(e, e.Pos())
			cond := l.expr(e)
			if tag != nil {
				t := tag()
				if cond.Type != t.Type {
					l.fail(e, "case value of type %s for a tag of type %s", cond.Type, t.Type)
				}
				cond = l.compare(ir.Eq, t, cond, pos)
			} else if cond.Type != ir.Bool {
				l.fail(e, "case condition of type %s", cond.Type)
			}
			next := l.fn.NewBlock()
			l.fn.Branch(l.b, cond, bodies[i], next, pos)
			l.b = next
		}
	}
	l.fn.Jump(l.b, dfltBody, l.pos(s, s.Switch))
	sw := &target{exit: exit}
	l.targets = append(l.targets, sw)
	done := dflt != nil
	for i, c := range s.Body.List {
		cc := c.(*ast.CaseClause)
		l.b, l.done = bodies[i], false
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

// varSpec lowers "var a, b T = x, y" and "var a, b T". All values are
// evaluated, left to right, before any is stored. A variable without an
// initializer is explicitly stored its zero value, 0 or false, since slots
// start undefined.
func (l *lowerer) varSpec(spec *ast.ValueSpec) {
	if len(spec.Values) == 0 {
		for _, id := range spec.Names {
			pos := l.pos(id, id.Pos())
			// Every zero value, the empty string's (0, 0) included, is
			// all zero words.
			for _, slot := range l.define(id) {
				l.fn.Store(l.b, slot, l.fn.TypedConst(l.b, slot.Type, 0, pos), pos)
			}
		}
		return
	}
	vals := l.valuesOrResults(spec.Values, len(spec.Names))
	for i, id := range spec.Names {
		l.store(variable{slots: l.define(id)}, vals[i], l.pos(id, id.Pos()))
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
		vals := l.valuesOrResults(s.Rhs, len(s.Lhs))
		for i, e := range s.Lhs {
			id := l.ident(e)
			var v variable
			if s.Tok == token.DEFINE && l.pkg.Info.Defs[id] != nil {
				v.slots = l.define(id)
			} else {
				v = l.lookup(id)
			}
			l.store(v, vals[i], l.pos(id, id.Pos()))
		}
	case token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN, token.QUO_ASSIGN, token.REM_ASSIGN,
		token.AND_ASSIGN, token.OR_ASSIGN, token.XOR_ASSIGN, token.AND_NOT_ASSIGN, token.SHL_ASSIGN, token.SHR_ASSIGN:
		if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
			l.fail(s, "compound assignment with %d targets and %d values", len(s.Lhs), len(s.Rhs))
		}
		op := map[token.Token]ir.Op{
			token.ADD_ASSIGN: ir.Add, token.SUB_ASSIGN: ir.Sub, token.MUL_ASSIGN: ir.Mul,
			token.AND_ASSIGN: ir.And, token.OR_ASSIGN: ir.Or, token.XOR_ASSIGN: ir.Xor, token.AND_NOT_ASSIGN: ir.AndNot,
			token.QUO_ASSIGN: ir.Div, token.REM_ASSIGN: ir.Rem, token.SHL_ASSIGN: ir.Shl, token.SHR_ASSIGN: ir.Shr,
		}[s.Tok]
		id := l.ident(s.Lhs[0])
		v := l.lookup(id)
		// The right side may branch (a call argument with && or ||), so x
		// is kept across it. If the variable is a global, the right side
		// may call a function that assigns it; when x is read relative to
		// that call is unspecified (spec §Order of evaluation), and
		// lowering reads it first, left to right.
		pos := l.pos(id, id.Pos())
		x := l.scalar(id, l.load(v, pos))
		get := func() *ir.Instr { return x }
		if l.branches(s.Rhs) {
			get = l.spill(x, pos)
		}
		y := l.expr(s.Rhs[0])
		l.store(v, []*ir.Instr{l.arith(op, get(), y, s.Rhs[0], l.pos(s, s.TokPos))}, pos)
	default:
		l.fail(s, "unexpected assignment %s", s.Tok)
	}
}

// exprStmt lowers a call statement. For print and println, the arguments
// are evaluated left to right, then printed by a single instruction. A call
// of a function discards its result, if any.
func (l *lowerer) exprStmt(s *ast.ExprStmt) {
	call, ok := s.X.(*ast.CallExpr)
	if !ok || call.Ellipsis.IsValid() {
		l.fail(s, "unexpected expression statement")
	}
	if id, ok := call.Fun.(*ast.Ident); ok {
		if b, ok := l.pkg.Info.Uses[id].(*types.Builtin); ok && (b.Name() == "println" || b.Name() == "print") {
			n := len(call.Args)
			if n == 1 {
				if t, ok := l.pkg.Info.Types[call.Args[0]].Type.(*types.Tuple); ok {
					n = t.Len() // println(f()) prints every result of f
				}
			}
			args, pos := flatten(l.valuesOrResults(call.Args, n)), l.pos(call, call.Pos())
			if b.Name() == "print" {
				l.fn.Print(l.b, args, pos)
			} else {
				l.fn.Println(l.b, args, pos)
			}
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
	f := l.funcs[obj]
	n := obj.Type().(*types.Signature).Params().Len()
	return l.fn.Call(l.b, f, flatten(l.valuesOrResults(call.Args, n)), l.pos(call, call.Pos()))
}

// results returns the Go values a call produces, from the IR call v: its
// single word, or its words extracted right after it, in its block, and
// grouped by its Go results.
func (l *lowerer) results(call *ast.CallExpr, v *ir.Instr) [][]*ir.Instr {
	var ts []types.Type
	switch t := l.pkg.Info.Types[call].Type.(type) {
	case *types.Tuple:
		for r := range t.Variables() {
			ts = append(ts, r.Type())
		}
	default:
		ts = []types.Type{t}
	}
	if v.Type != ir.Tuple {
		if len(ts) != 1 || len(l.words(call, ts[0])) != 1 {
			l.fail(call, "call has one word for %d results", len(ts))
		}
		return [][]*ir.Instr{{v}}
	}
	var vals [][]*ir.Instr
	i := 0
	for _, t := range ts {
		var w []*ir.Instr
		for range l.words(call, t) {
			if i >= len(v.Callee.Results) {
				l.fail(call, "call has too few result words")
			}
			w = append(w, l.fn.Extract(l.b, v, i, l.pos(call, call.Pos())))
			i++
		}
		vals = append(vals, w)
	}
	if i != len(v.Callee.Results) {
		l.fail(call, "call has %d result words, its type %d", len(v.Callee.Results), i)
	}
	return vals
}

// flatten returns the words of vals, in order.
func flatten(vals [][]*ir.Instr) []*ir.Instr {
	var out []*ir.Instr
	for _, w := range vals {
		out = append(out, w...)
	}
	return out
}

// scalar returns the one word of a Go value that has one.
func (l *lowerer) scalar(n ast.Node, w []*ir.Instr) *ir.Instr {
	if len(w) != 1 {
		l.fail(n, "a value of %d words where one is needed", len(w))
	}
	return w[0]
}

// valuesOrResults lowers list, which supplies n values: either n
// expressions (values), or a single call with n results, as in "return
// f()", "a, b := f()", and "g(f())". The results are extracted right after
// the call, in its block.
func (l *lowerer) valuesOrResults(list []ast.Expr, n int) [][]*ir.Instr {
	if len(list) == n {
		return l.values(list)
	}
	call, ok := list[0].(*ast.CallExpr)
	if len(list) != 1 || !ok || n < 2 {
		l.fail(list[0], "%d values where %d are needed", len(list), n)
	}
	vals := l.results(call, l.call(call))
	if len(vals) != n {
		l.fail(call, "call does not have %d results", n)
	}
	return vals
}

// ident returns e as the identifier of an assignment target.
func (l *lowerer) ident(e ast.Expr) *ast.Ident {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" {
		l.fail(e, "unexpected assignment target %s", types.ExprString(e))
	}
	return id
}

// define creates the slots, one per word, for the variable that id
// declares.
func (l *lowerer) define(id *ast.Ident) []*ir.Slot {
	v, ok := l.pkg.Info.Defs[id].(*types.Var)
	if !ok {
		l.fail(id, "%s does not declare a variable", id.Name)
	}
	if l.slots[v] != nil {
		l.fail(id, "variable %s declared twice", id.Name)
	}
	for _, t := range l.words(id, v.Type()) {
		l.slots[v] = append(l.slots[v], l.fn.NewSlot(id.Name, t, l.pos(id, id.Pos())))
	}
	return l.slots[v]
}

// variable is the storage of a variable, one slot or global per word:
// slots if it is local, or globals if it is declared at package level.
type variable struct {
	slots   []*ir.Slot
	globals []*ir.Global
}

// lookup returns the storage of the variable that id uses.
func (l *lowerer) lookup(id *ast.Ident) variable {
	v, ok := l.pkg.Info.Uses[id].(*types.Var)
	switch {
	case !ok:
	case l.slots[v] != nil:
		return variable{slots: l.slots[v]}
	case l.globals[v] != nil:
		return variable{globals: l.globals[v]}
	}
	l.fail(id, "%s does not denote a variable", id.Name)
	return variable{}
}

// load returns the words of v.
func (l *lowerer) load(v variable, pos ir.Pos) []*ir.Instr {
	var w []*ir.Instr
	for _, g := range v.globals {
		w = append(w, l.fn.GLoad(l.b, g, pos))
	}
	for _, s := range v.slots {
		w = append(w, l.fn.Load(l.b, s, pos))
	}
	return w
}

// store stores the words w in v, in order.
func (l *lowerer) store(v variable, w []*ir.Instr, pos ir.Pos) {
	if len(w) != len(v.globals)+len(v.slots) {
		l.fail(nil, "storing %d words in a variable of %d", len(w), len(v.globals)+len(v.slots))
	}
	for i, g := range v.globals {
		l.fn.GStore(l.b, g, w[i], pos)
	}
	for i, s := range v.slots {
		l.fn.Store(l.b, s, w[i], pos)
	}
}

// values lowers list left to right. A value computed before a later
// operand that branches is stored in a temporary slot and loaded again
// after the last operand, in the block where the values are used.
func (l *lowerer) values(list []ast.Expr) [][]*ir.Instr {
	// later[i] reports whether an operand after list[i] branches. It is
	// computed in one pass, since println may have very many operands.
	later := make([]bool, len(list))
	for i := len(list) - 2; i >= 0; i-- {
		later[i] = later[i+1] || l.branches(list[i+1:i+2])
	}
	get := make([]func() []*ir.Instr, len(list))
	for i, e := range list {
		w, pos := l.value(e), l.pos(e, e.Pos())
		get[i] = func() []*ir.Instr { return w }
		if later[i] {
			get[i] = l.spillWords(w, pos)
		}
	}
	vals := make([][]*ir.Instr, len(list))
	for i := range get {
		vals[i] = get[i]()
	}
	return vals
}

// spillWords is spill for each word of a Go value.
func (l *lowerer) spillWords(w []*ir.Instr, pos ir.Pos) func() []*ir.Instr {
	gets := make([]func() *ir.Instr, len(w))
	for i, v := range w {
		gets[i] = l.spill(v, pos)
	}
	return func() []*ir.Instr {
		out := make([]*ir.Instr, len(gets))
		for i, get := range gets {
			out[i] = get()
		}
		return out
	}
}

// spill stores v in a temporary slot and returns a function that loads it
// again in the then current block.
func (l *lowerer) spill(v *ir.Instr, pos ir.Pos) func() *ir.Instr {
	tmp := l.fn.NewSlot("tmp", v.Type, pos)
	l.fn.Store(l.b, tmp, v, pos)
	return func() *ir.Instr { return l.fn.Load(l.b, tmp, pos) }
}

// branches reports whether lowering any of list might branch: whether it
// contains a non-constant &&, ||, division, shift, or index expression.
func (l *lowerer) branches(list []ast.Expr) bool {
	found := false
	for _, e := range list {
		ast.Inspect(e, func(n ast.Node) bool {
			if e, ok := n.(ast.Expr); ok {
				if tv, ok := l.pkg.Info.Types[e]; ok && tv.Value != nil {
					return false // a constant: lowered as one const
				}
			}
			if b, ok := n.(*ast.BinaryExpr); ok {
				switch b.Op {
				case token.LAND, token.LOR, token.QUO, token.REM, token.SHL, token.SHR:
					found = true // may branch: a conservative test
				}
			}
			if _, ok := n.(*ast.IndexExpr); ok {
				found = true // the bounds check
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
		if t.IsFloat() {
			return l.floatConst(e, t, tv.Value, pos)
		}
		// go/types has checked that the value is representable in its
		// type, so it is already in canonical form: unsigned values above
		// the int64 range keep their 64-bit pattern.
		var n int64
		exact := false
		// An integer constant may be written as a float literal with an
		// integral value, as in "var i int = 1.0"; ToInt converts it
		// exactly, and leaves any other value not an Int.
		if v := constant.ToInt(tv.Value); v.Kind() == constant.Int {
			tv.Value = v
			if t.Signed() {
				n, exact = constant.Int64Val(tv.Value)
			} else {
				var u uint64
				u, exact = constant.Uint64Val(tv.Value)
				n = int64(u)
			}
		}
		if !exact || t.Canonical(n) != n {
			l.fail(e, "constant %s is not representable as %s", tv.Value, t)
		}
		return l.fn.TypedConst(l.b, t, n, pos)
	}
	switch e := e.(type) {
	case *ast.Ident:
		return l.scalar(e, l.load(l.lookup(e), l.pos(e, e.Pos())))
	case *ast.ParenExpr:
		return l.expr(e.X)
	case *ast.IndexExpr:
		return l.index(e)
	case *ast.CallExpr:
		if id, ok := ast.Unparen(e.Fun).(*ast.Ident); ok {
			if b, ok := l.pkg.Info.Uses[id].(*types.Builtin); ok && b.Name() == "len" && len(e.Args) == 1 {
				// The length of a string is its second word: its bytes.
				w := l.value(e.Args[0])
				if len(w) != 2 {
					l.fail(e, "len of a value of %d words", len(w))
				}
				return w[1]
			}
		}
		if l.pkg.Info.Types[e.Fun].IsType() {
			// A conversion: between integer types a conv, and between
			// identical types, such as bool(b), nothing.
			x := l.expr(e.Args[0])
			pos := l.pos(e, e.Pos())
			switch {
			case x.Type == t:
				return x
			case x.Type.IsInteger() && t.IsInteger():
				return l.fn.Conv(l.b, t, x, pos)
			case x.Type.IsInteger() && t.IsFloat():
				return l.fn.Convert(l.b, ir.IToF, t, x, pos)
			case x.Type.IsFloat() && t.IsInteger():
				return l.fn.Convert(l.b, ir.FToI, t, x, pos)
			case x.Type.IsFloat() && t.IsFloat():
				return l.fn.Convert(l.b, ir.FToF, t, x, pos)
			}
			l.fail(e, "conversion from %s to %s", x.Type, t)
		}
		v := l.scalar(e, flatten(l.results(e, l.call(e))))
		if v.Type != t {
			l.fail(e, "call of type %s used as a value of type %s", v.Type, t)
		}
		return v
	case *ast.UnaryExpr:
		switch e.Op {
		case token.ADD:
			return l.expr(e.X)
		case token.SUB:
			x := l.expr(e.X)
			if x.Type.IsFloat() {
				return l.fn.Float(l.b, ir.FNeg, l.pos(e, e.OpPos), x)
			}
			return l.fn.Neg(l.b, x, l.pos(e, e.OpPos))
		case token.NOT:
			return l.fn.Not(l.b, l.expr(e.X), l.pos(e, e.OpPos))
		case token.XOR:
			return l.fn.Com(l.b, l.expr(e.X), l.pos(e, e.OpPos))
		}
	case *ast.BinaryExpr:
		pos := l.pos(e, e.OpPos)
		switch e.Op {
		case token.LAND, token.LOR:
			return l.logical(e, pos)
		}
		if op, ok := binaryOps[e.Op]; ok {
			xy := l.values([]ast.Expr{e.X, e.Y})
			x, y := l.scalar(e.X, xy[0]), l.scalar(e.Y, xy[1])
			if op.IsComparison() {
				return l.compare(op, x, y, pos)
			}
			return l.arith(op, x, y, e.Y, pos)
		}
	}
	l.fail(e, "unexpected expression %s", types.ExprString(e))
	return nil
}

// value lowers an expression of any supported type and returns its words.
func (l *lowerer) value(e ast.Expr) []*ir.Instr {
	tv, ok := l.pkg.Info.Types[e]
	if !ok {
		l.fail(e, "no type for expression %s", types.ExprString(e))
	}
	if !isString(tv.Type) {
		return []*ir.Instr{l.expr(e)}
	}
	pos := l.pos(e, e.Pos())
	if tv.Value != nil {
		// A constant: its bytes, interned once per program, or the empty
		// string's (0, 0) (D-033).
		if tv.Value.Kind() != constant.String {
			l.fail(e, "string constant of kind %s", tv.Value.Kind())
		}
		str := constant.StringVal(tv.Value)
		if str == "" {
			return []*ir.Instr{l.fn.TypedConst(l.b, ir.Ptr, 0, pos), l.fn.Const(l.b, 0, pos)}
		}
		return []*ir.Instr{l.fn.Str(l.b, l.prog.Intern(str), pos), l.fn.Const(l.b, int64(len(str)), pos)}
	}
	switch e := e.(type) {
	case *ast.Ident:
		return l.load(l.lookup(e), pos)
	case *ast.ParenExpr:
		return l.value(e.X)
	case *ast.CallExpr:
		if !l.pkg.Info.Types[e.Fun].IsType() {
			vals := l.results(e, l.call(e))
			if len(vals) != 1 {
				l.fail(e, "call of %d results used as one value", len(vals))
			}
			return vals[0]
		}
	}
	l.fail(e, "unexpected string expression %s", types.ExprString(e))
	return nil
}

func isString(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && (b.Kind() == types.String || b.Kind() == types.UntypedString)
}

// index lowers s[i], a byte of a string. The string is evaluated, then the
// index; the index, as an unsigned 64-bit number, must be below the length,
// so that a negative one fails too, or the program panics with the index, as
// written, and the length (D-033). Both blocks reload the values they need
// from temporary slots, since values are local to their block.
func (l *lowerer) index(e *ast.IndexExpr) *ir.Instr {
	if !isString(l.pkg.Info.Types[e.X].Type) {
		l.fail(e, "index of a non-string")
	}
	pos := l.pos(e, e.Lbrack)
	w := l.value(e.X)
	get := func() []*ir.Instr { return w }
	if l.branches([]ast.Expr{e.Index}) {
		get = l.spillWords(w, pos)
	}
	i := l.expr(e.Index)
	w = get()
	if !i.Type.IsInteger() {
		l.fail(e.Index, "index of type %s", i.Type)
	}
	u, n := i, w[1]
	if u.Type != ir.U64 {
		u = l.fn.Conv(l.b, ir.U64, u, pos)
	}
	fail := l.fn.Compare(l.b, ir.Ge, u, l.fn.Conv(l.b, ir.U64, n, pos), pos)
	ptr, idx, length := l.spill(w[0], pos), l.spill(i, pos), l.spill(n, pos)
	panicking, ok := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Branch(l.b, fail, panicking, ok, pos)
	l.b = panicking
	l.fn.Panic(panicking, ir.PanicIndex, pos, idx(), length())
	l.b = ok
	p, x := ptr(), idx()
	if x.Type != ir.I64 {
		// Below the length, so the same number as an I64.
		x = l.fn.Conv(l.b, ir.I64, x, pos)
	}
	return l.fn.LoadB(l.b, p, x, pos)
}

var binaryOps = map[token.Token]ir.Op{
	token.ADD: ir.Add, token.SUB: ir.Sub, token.MUL: ir.Mul,
	token.AND: ir.And, token.OR: ir.Or, token.XOR: ir.Xor, token.AND_NOT: ir.AndNot,
	token.QUO: ir.Div, token.REM: ir.Rem, token.SHL: ir.Shl, token.SHR: ir.Shr,
	token.EQL: ir.Eq, token.NEQ: ir.Ne, token.LSS: ir.Lt, token.LEQ: ir.Le, token.GTR: ir.Gt, token.GEQ: ir.Ge,
}

// arith appends the arithmetic operation op of x and y, where y is the
// value of the expression ye. A division first checks that y is not 0, and
// a shift by a signed count that it is not negative, branching to a block
// that panics if it fails (D-023 explicit checks); the count is then
// converted to u64. A constant y needs no check: go/types rejects a
// constant divisor of 0 and a negative constant count.
func (l *lowerer) arith(op ir.Op, x, y *ir.Instr, ye ast.Expr, pos ir.Pos) *ir.Instr {
	if x.Type.IsFloat() {
		// IEEE 754 division by zero is defined, so a float operation
		// needs no check (D-032).
		fop, ok := floatOps[op]
		if !ok || y.Type != x.Type {
			l.fail(ye, "%s of %s and %s", op, x.Type, y.Type)
		}
		return l.fn.Float(l.b, fop, pos, x, y)
	}
	constant := l.pkg.Info.Types[ye].Value != nil
	switch op {
	case ir.Div, ir.Rem:
		if !constant {
			zero := l.fn.Compare(l.b, ir.Eq, y, l.fn.TypedConst(l.b, y.Type, 0, pos), pos)
			v := l.guard(zero, ir.PanicDivide, pos, x, y)
			x, y = v[0], v[1]
		}
	case ir.Shl, ir.Shr:
		if y.Type.Signed() {
			if !constant {
				neg := l.fn.Compare(l.b, ir.Lt, y, l.fn.TypedConst(l.b, y.Type, 0, pos), pos)
				v := l.guard(neg, ir.PanicShift, pos, x, y)
				x, y = v[0], v[1]
			}
			y = l.fn.Conv(l.b, ir.U64, y, pos)
		}
		return l.fn.Shift(l.b, op, x, y, pos)
	}
	return l.fn.Binary(l.b, op, x, y, pos)
}

// floatOps maps the arithmetic and comparison operations of the integer
// types to those of the float types, which are distinct (D-032).
var floatOps = map[ir.Op]ir.Op{
	ir.Add: ir.FAdd, ir.Sub: ir.FSub, ir.Mul: ir.FMul, ir.Div: ir.FDiv,
	ir.Eq: ir.FEq, ir.Ne: ir.FNe, ir.Lt: ir.FLt, ir.Le: ir.FLe, ir.Gt: ir.FGt, ir.Ge: ir.FGe,
}

// compare appends the comparison op of x and y: the float comparison if
// they are floats.
func (l *lowerer) compare(op ir.Op, x, y *ir.Instr, pos ir.Pos) *ir.Instr {
	if x.Type.IsFloat() {
		return l.fn.Float(l.b, floatOps[op], pos, x, y)
	}
	return l.fn.Compare(l.b, op, x, y, pos)
}

// floatConst appends the constant v as a float of type t. go/types has
// rounded a typed constant to its type, so its value converts exactly; an
// inexact result, or an infinity, is an internal error.
func (l *lowerer) floatConst(e ast.Expr, t ir.Type, v constant.Value, pos ir.Pos) *ir.Instr {
	var f float64
	exact := false
	if t == ir.F32 {
		var f32 float32
		f32, exact = constant.Float32Val(v)
		f = float64(f32)
	} else {
		f, exact = constant.Float64Val(v)
	}
	if !exact || math.IsInf(f, 0) {
		l.fail(e, "constant %s is not representable as %s", v, t)
	}
	return l.fn.FloatConst(l.b, t, f, pos)
}

// guard branches to a new block that panics with kind if fail is true, and
// continues in another block otherwise. vals, computed in the current
// block, are kept in temporary slots across the branch and returned as
// loads in the new current block.
func (l *lowerer) guard(fail *ir.Instr, kind int, pos ir.Pos, vals ...*ir.Instr) []*ir.Instr {
	get := make([]func() *ir.Instr, len(vals))
	for i, v := range vals {
		get[i] = l.spill(v, pos)
	}
	panicking, ok := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Branch(l.b, fail, panicking, ok, pos)
	l.fn.Panic(panicking, kind, pos)
	l.b = ok
	out := make([]*ir.Instr, len(vals))
	for i := range get {
		out[i] = get[i]()
	}
	return out
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

// irTypes maps the predeclared bool and integer types to IR types. int,
// uint, and uintptr are 64 bits wide (types.SizesFor("gc", "amd64")). An
// untyped constant that keeps its untyped type has its default type.
var irTypes = map[types.BasicKind]ir.Type{
	types.Bool: ir.Bool, types.UntypedBool: ir.Bool,
	types.Int: ir.I64, types.Int64: ir.I64, types.UntypedInt: ir.I64,
	types.Int8: ir.I8, types.Int16: ir.I16, types.Int32: ir.I32, types.UntypedRune: ir.I32,
	types.Uint: ir.U64, types.Uint64: ir.U64, types.Uintptr: ir.U64,
	types.Uint8: ir.U8, types.Uint16: ir.U16, types.Uint32: ir.U32,
	types.Float64: ir.F64, types.UntypedFloat: ir.F64, types.Float32: ir.F32,
}

// words returns the IR types of the words of a Go type: Ptr and I64 for a
// string (D-033), and its one IR type otherwise.
func (l *lowerer) words(n ast.Node, t types.Type) []ir.Type {
	if isString(t) {
		return []ir.Type{ir.Ptr, ir.I64}
	}
	return []ir.Type{l.irType(n, t)}
}

// irType returns the IR type of a bool or integer Go type.
func (l *lowerer) irType(n ast.Node, t types.Type) ir.Type {
	if b, ok := t.(*types.Basic); ok {
		if it, ok := irTypes[b.Kind()]; ok {
			return it
		}
	}
	l.fail(n, "unexpected type %s", t)
	return ir.NoType
}
