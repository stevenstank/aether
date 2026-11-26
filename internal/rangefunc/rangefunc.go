// Package rangefunc rewrites range-over-function loops (Go 1.23) into
// calls of the iterator with a function literal, at the source level,
// after a package's last type check and before the gate and lowering
// (v2 evaluation, D-055).
//
// A loop
//
//	L: for k, v := range seq { body }
//
// becomes the block
//
//	{
//		var #state int    // 0 ready, 1 the body ended the loop, 2 exited, 3 in the body
//		var #ret int      // 1 if the body returned (only if it may)
//		var #r0 T0 ...    // the results it returned, if unnamed
//		seq(func(k K, v V) bool {
//			if #state == 1 { panic("range function continued iteration after function for loop body returned false") }
//			if #state == 2 { panic("range function continued iteration after whole loop exit") }
//			#state = 3
//			body'
//			#state = 0; return true
//		})
//		if #state == 3 { panic("range function recovered a loop body panic and did not resume panicking") }
//		#state = 2
//		if #ret == 1 { return #r0, ... }
//	}
//
// where body' is body with each continue of the loop "#state = 0; return true", each
// break of it "#state = 1; return false", and each return "#r0, ... =
// results; #ret = 1; #state = 1; return false" (assigning named results
// directly). With "=" in place of ":=", the literal's parameters are new
// variables assigned to k and v first. Loops nested in body are rewritten
// first, so the returns their rewriting adds are rewritten in turn.
//
// The synthesized nodes are recorded in the package's types.Info as
// go/types would record them: the loop's own variables are the literal's
// parameters, and new variables are declared just before the loop, outside
// the literal, so that it captures them (D-040). A body with a defer, a
// goto out of it, or a break or continue of a loop around it is left as
// it is, and reported unsupported.
package rangefunc

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
)

// Feature is the gate's feature name of an unsupported range statement.
const Feature = "for-range"

// The messages of gc's run-time checks of an iterator (runtime/panic.go,
// go1.26.1), which gc raises as run-time errors.
const (
	msgAfterFalse = "runtime error: range function continued iteration after function for loop body returned false"
	msgAfterExit  = "runtime error: range function continued iteration after whole loop exit"
	msgRecovered  = "runtime error: range function recovered a loop body panic and did not resume panicking"
)

// protos are type-and-value records of go/types, whose modes are not
// exported, harvested from a small checked program: of the type names int
// and bool, of the builtin panic, and of a call of it.
type protos struct {
	intType, boolType, panicFn, panicCall, strArg types.TypeAndValue
}

func harvest() protos {
	const src = "package p\nvar _ int\nvar _ bool\nfunc f() { panic(\"\") }\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		panic(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if _, err := (&types.Config{}).Check("p", fset, []*ast.File{f}, info); err != nil {
		panic(err)
	}
	var p protos
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			switch n.Name {
			case "int":
				p.intType = info.Types[n]
			case "bool":
				p.boolType = info.Types[n]
			case "panic":
				p.panicFn = info.Types[n]
			}
		case *ast.CallExpr:
			p.panicCall = info.Types[n]
			p.strArg = info.Types[n.Args[0]]
		}
		return true
	})
	return p
}

type rewriter struct {
	pkg    *frontend.Package
	info   *types.Info
	p      protos
	reject func(pos diag.Pos, feature, what string) diag.Diagnostic
	ds     []diag.Diagnostic
	n      int // loops rewritten, for the names of new variables
}

// fnCtx is the function whose body is being rewritten.
type fnCtx struct {
	sig     *types.Signature
	results *ast.FieldList
	named   []*types.Var // its named results, or nil
}

// Rewrite rewrites the range-over-function loops of pkg in place, and
// returns the diagnostics of those it cannot.
func Rewrite(pkg *frontend.Package, reject func(pos diag.Pos, feature, what string) diag.Diagnostic) []diag.Diagnostic {
	r := &rewriter{pkg: pkg, info: pkg.Info, reject: reject}
	type fn struct {
		ctx  *fnCtx
		body *ast.BlockStmt
	}
	var fns []fn
	found := false
	for _, f := range pkg.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				if n.Body != nil {
					if obj, ok := r.info.Defs[n.Name].(*types.Func); ok {
						fns = append(fns, fn{r.ctx(obj.Type().(*types.Signature), n.Type.Results), n.Body})
					}
				}
			case *ast.FuncLit:
				if sig, ok := r.info.Types[n].Type.(*types.Signature); ok {
					fns = append(fns, fn{r.ctx(sig, n.Type.Results), n.Body})
				}
			case *ast.RangeStmt:
				found = found || r.overFunc(n)
			}
			return true
		})
	}
	if !found {
		return nil
	}
	r.p = harvest()
	for _, f := range fns {
		r.stmts(f.body.List, f.ctx)
	}
	return r.ds
}

func (r *rewriter) ctx(sig *types.Signature, results *ast.FieldList) *fnCtx {
	c := &fnCtx{sig: sig, results: results}
	if results != nil && len(results.List) > 0 && len(results.List[0].Names) > 0 {
		for _, f := range results.List {
			for _, id := range f.Names {
				c.named = append(c.named, r.info.Defs[id].(*types.Var))
			}
		}
	}
	return c
}

// overFunc reports whether s ranges over a function.
func (r *rewriter) overFunc(s *ast.RangeStmt) bool {
	tv, ok := r.info.Types[s.X]
	if !ok || tv.Type == nil {
		return false
	}
	_, ok = tv.Type.Underlying().(*types.Signature)
	return ok
}

func (r *rewriter) stmts(list []ast.Stmt, fc *fnCtx) {
	for i, s := range list {
		list[i] = r.stmt(s, fc)
	}
}

// stmt rewrites the loops in s, innermost first, and returns s or its
// replacement. It does not enter function literals, which are rewritten
// as functions of their own.
func (r *rewriter) stmt(s ast.Stmt, fc *fnCtx) ast.Stmt {
	switch s := s.(type) {
	case *ast.BlockStmt:
		r.stmts(s.List, fc)
	case *ast.IfStmt:
		r.stmt(s.Body, fc)
		if s.Else != nil {
			s.Else = r.stmt(s.Else, fc)
		}
	case *ast.ForStmt:
		r.stmt(s.Body, fc)
	case *ast.RangeStmt:
		r.stmt(s.Body, fc)
		if r.overFunc(s) {
			return r.rewrite(s, nil, fc)
		}
	case *ast.SwitchStmt:
		r.stmt(s.Body, fc)
	case *ast.TypeSwitchStmt:
		r.stmt(s.Body, fc)
	case *ast.SelectStmt:
		r.stmt(s.Body, fc)
	case *ast.CaseClause:
		r.stmts(s.Body, fc)
	case *ast.CommClause:
		r.stmts(s.Body, fc)
	case *ast.LabeledStmt:
		if rs, ok := s.Stmt.(*ast.RangeStmt); ok && r.overFunc(rs) {
			r.stmt(rs.Body, fc)
			if b := r.rewrite(rs, s.Label, fc); b != rs {
				return b // the label's branches are rewritten
			}
			return s
		}
		s.Stmt = r.stmt(s.Stmt, fc)
	}
	return s
}

// Constructors of typed nodes.

func (r *rewriter) record(e ast.Expr, t types.Type, v constant.Value) {
	r.info.Types[e] = types.TypeAndValue{Type: t, Value: v}
}

func (r *rewriter) use(v *types.Var, pos token.Pos) *ast.Ident {
	id := &ast.Ident{NamePos: pos, Name: v.Name()}
	r.info.Uses[id] = v
	r.record(id, v.Type(), nil)
	return id
}

func (r *rewriter) intLit(n int64, pos token.Pos) *ast.BasicLit {
	b := &ast.BasicLit{ValuePos: pos, Kind: token.INT, Value: fmt.Sprint(n)}
	r.record(b, types.Typ[types.Int], constant.MakeInt64(n))
	return b
}

func (r *rewriter) boolLit(b bool, pos token.Pos) *ast.Ident {
	name := "false"
	if b {
		name = "true"
	}
	id := &ast.Ident{NamePos: pos, Name: name}
	r.info.Uses[id] = types.Universe.Lookup(name)
	r.record(id, types.Typ[types.Bool], constant.MakeBool(b))
	return id
}

func (r *rewriter) typeName(name string, tv types.TypeAndValue, pos token.Pos) *ast.Ident {
	id := &ast.Ident{NamePos: pos, Name: name}
	r.info.Uses[id] = types.Universe.Lookup(name)
	r.info.Types[id] = tv
	return id
}

// newVar declares a variable of type t at pos, in a scope of its own, and
// returns it with its defining identifier.
func (r *rewriter) newVar(name string, t types.Type, pos token.Pos) (*types.Var, *ast.Ident) {
	v := types.NewVar(pos, r.pkg.Types, name, t)
	types.NewScope(nil, pos, pos, "rangefunc").Insert(v) // a local: its parent
	id := &ast.Ident{NamePos: pos, Name: name}
	r.info.Defs[id] = v
	return v, id
}

func (r *rewriter) varDecl(id *ast.Ident, typ ast.Expr, pos token.Pos) ast.Stmt {
	return &ast.DeclStmt{Decl: &ast.GenDecl{TokPos: pos, Tok: token.VAR,
		Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{id}, Type: typ}}}}
}

func (r *rewriter) assign(lhs, rhs []ast.Expr, pos token.Pos) ast.Stmt {
	return &ast.AssignStmt{Lhs: lhs, TokPos: pos, Tok: token.ASSIGN, Rhs: rhs}
}

func (r *rewriter) setInt(v *types.Var, n int64, pos token.Pos) ast.Stmt {
	return r.assign([]ast.Expr{r.use(v, pos)}, []ast.Expr{r.intLit(n, pos)}, pos)
}

func (r *rewriter) eqInt(v *types.Var, n int64, pos token.Pos) ast.Expr {
	b := &ast.BinaryExpr{X: r.use(v, pos), OpPos: pos, Op: token.EQL, Y: r.intLit(n, pos)}
	r.record(b, types.Typ[types.UntypedBool], nil)
	return b
}

func (r *rewriter) panicStmt(msg string, pos token.Pos) ast.Stmt {
	fn := &ast.Ident{NamePos: pos, Name: "panic"}
	r.info.Uses[fn] = types.Universe.Lookup("panic")
	r.info.Types[fn] = r.p.panicFn
	arg := &ast.BasicLit{ValuePos: pos, Kind: token.STRING, Value: fmt.Sprintf("%q", msg)}
	tv := r.p.strArg
	tv.Value = constant.MakeString(msg)
	r.info.Types[arg] = tv
	call := &ast.CallExpr{Fun: fn, Lparen: pos, Args: []ast.Expr{arg}, Rparen: pos}
	r.info.Types[call] = r.p.panicCall
	return &ast.ExprStmt{X: call}
}

func (r *rewriter) ifStmt(cond ast.Expr, body []ast.Stmt, pos token.Pos) ast.Stmt {
	return &ast.IfStmt{If: pos, Cond: cond, Body: &ast.BlockStmt{Lbrace: pos, List: body, Rbrace: pos}}
}

func (r *rewriter) ret(results []ast.Expr, pos token.Pos) ast.Stmt {
	return &ast.ReturnStmt{Return: pos, Results: results}
}

// loop is the rewriting of one loop.
type loop struct {
	label   *ast.Ident
	labels  map[string]bool // the labels declared in its body
	state   *types.Var
	retVar  *types.Var        // #ret, made when the body returns or leaves
	exits   []*ast.BranchStmt // branches out of the body, codes 2 and on
	returns bool
	results []*types.Var // the unnamed results' holders
	fc      *fnCtx
	bad     bool
}

// rewrite returns the block replacing s, labeled label if not nil, or s
// itself, reported, if its body cannot be rewritten.
func (r *rewriter) rewrite(s *ast.RangeStmt, label *ast.Ident, fc *fnCtx) ast.Stmt {
	sig := r.info.Types[s.X].Type.Underlying().(*types.Signature)
	if sig.Params().Len() != 1 {
		r.fail(s.Pos(), "range over a function of an unexpected type")
		return s
	}
	yield, ok := sig.Params().At(0).Type().Underlying().(*types.Signature)
	if !ok {
		r.fail(s.Pos(), "range over a function of an unexpected type")
		return s
	}
	r.n++
	before := s.For - 1 // the new variables' position: outside the literal
	if s.For <= 1 {
		before = s.For
	}
	end := s.Body.Rbrace
	lp := &loop{label: label, labels: map[string]bool{}, fc: fc}
	ast.Inspect(s.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.LabeledStmt:
			lp.labels[n.Label.Name] = true
		}
		return true
	})
	var stateID *ast.Ident
	lp.state, stateID = r.newVar(fmt.Sprintf("#state%d", r.n), types.Typ[types.Int], before)
	decls := []ast.Stmt{r.varDecl(stateID, r.typeName("int", r.p.intType, before), before)}

	// The body, its branches and returns rewritten.
	body := r.body(s.Body.List, lp, 0, 0, before)
	if lp.bad {
		return s
	}
	if lp.retVar != nil {
		decls = append(decls, r.varDecl(r.defOf(lp.retVar), r.typeName("int", r.p.intType, before), before))
		for k, h := range lp.results {
			// Each unnamed result has a field of its own, whose type
			// expression go/types has recorded.
			decls = append(decls, r.varDecl(r.defOf(h), fc.results.List[k].Type, before))
		}
	}

	// The literal's parameters: the loop's variables, or new ones.
	var params []*ast.Field
	var prologue []ast.Stmt
	exprs := []ast.Expr{s.Key, s.Value}
	var assignL, assignR []ast.Expr
	for i := range yield.Params().Len() {
		pt := yield.Params().At(i).Type()
		var id *ast.Ident
		var e ast.Expr
		if i < len(exprs) {
			e = exprs[i]
		}
		switch {
		case e != nil && s.Tok == token.DEFINE:
			id = e.(*ast.Ident) // its Defs record is the loop variable
			if id.Name == "_" {
				id = &ast.Ident{NamePos: id.NamePos, Name: "_"}
			}
		case e != nil && !isBlank(e):
			v, def := r.newVar(fmt.Sprintf("#p%d_%d", r.n, i), pt, s.For)
			id = def
			assignL = append(assignL, e)
			assignR = append(assignR, r.use(v, s.For))
		default:
			id = &ast.Ident{NamePos: s.For, Name: "_"}
		}
		ft := &ast.Ident{NamePos: s.For, Name: types.TypeString(pt, nil)}
		r.record(ft, pt, nil)
		params = append(params, &ast.Field{Names: []*ast.Ident{id}, Type: ft})
	}
	if len(assignL) > 0 {
		prologue = append(prologue, r.assign(assignL, assignR, s.For))
	}
	check := []ast.Stmt{
		r.ifStmt(r.eqInt(lp.state, 1, s.For), []ast.Stmt{r.panicStmt(msgAfterFalse, s.For)}, s.For),
		r.ifStmt(r.eqInt(lp.state, 2, s.For), []ast.Stmt{r.panicStmt(msgAfterExit, s.For)}, s.For),
	}
	check = append(check, r.setInt(lp.state, 3, s.For))
	list := append(append(check, prologue...), body...)
	list = append(list, r.next(lp, end))
	boolT := r.typeName("bool", r.p.boolType, s.For)
	lit := &ast.FuncLit{
		Type: &ast.FuncType{Func: s.For, Params: &ast.FieldList{Opening: s.For, List: params, Closing: s.For},
			Results: &ast.FieldList{List: []*ast.Field{{Type: boolT}}}},
		Body: &ast.BlockStmt{Lbrace: s.Body.Lbrace, List: list, Rbrace: end},
	}
	r.record(lit, yield, nil)
	call := &ast.CallExpr{Fun: s.X, Lparen: s.For, Args: []ast.Expr{lit}, Rparen: end}
	r.record(call, types.NewTuple(), nil)

	out := append(decls, &ast.ExprStmt{X: call},
		r.ifStmt(r.eqInt(lp.state, 3, end), []ast.Stmt{r.panicStmt(msgRecovered, end)}, end),
		r.setInt(lp.state, 2, end))
	if lp.retVar != nil {
		var res []ast.Expr
		for _, h := range lp.results {
			res = append(res, r.use(h, end))
		}
		if lp.returns {
			out = append(out, r.ifStmt(r.eqInt(lp.retVar, 1, end), []ast.Stmt{r.ret(res, end)}, end))
		}
		for k, b := range lp.exits {
			out = append(out, r.ifStmt(r.eqInt(lp.retVar, int64(2+k), end), []ast.Stmt{b}, end))
		}
	}
	return &ast.BlockStmt{Lbrace: s.For, List: out, Rbrace: end}
}

// defOf returns a new defining identifier of v, which rewrite declares.
func (r *rewriter) defOf(v *types.Var) *ast.Ident {
	id := &ast.Ident{NamePos: v.Pos(), Name: v.Name()}
	r.info.Defs[id] = v
	return id
}

func isBlank(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}

func (r *rewriter) fail(pos token.Pos, what string) {
	r.ds = append(r.ds, r.reject(frontend.Position(r.pkg.Fset, pos), Feature, what))
}

// body rewrites the statements of a loop's body: loops counts the for and
// range statements around them in the body, breaks those and the switch
// and select statements.
func (r *rewriter) body(list []ast.Stmt, lp *loop, loops, breaks int, before token.Pos) []ast.Stmt {
	out := make([]ast.Stmt, len(list))
	for i, s := range list {
		out[i] = r.bodyStmt(s, lp, loops, breaks, before)
	}
	return out
}

func (r *rewriter) bodyStmt(s ast.Stmt, lp *loop, loops, breaks int, before token.Pos) ast.Stmt {
	switch s := s.(type) {
	case *ast.BranchStmt:
		ours := s.Label != nil && lp.label != nil && s.Label.Name == lp.label.Name
		inner := s.Label != nil && lp.labels[s.Label.Name]
		switch s.Tok {
		case token.CONTINUE:
			if (s.Label == nil && loops == 0) || ours {
				return r.next(lp, s.Pos())
			}
			if s.Label != nil && !inner {
				return r.exit(lp, s, before)
			}
		case token.BREAK:
			if (s.Label == nil && breaks == 0) || ours {
				return &ast.BlockStmt{Lbrace: s.Pos(), List: []ast.Stmt{
					r.setInt(lp.state, 1, s.Pos()), r.ret([]ast.Expr{r.boolLit(false, s.Pos())}, s.Pos())}, Rbrace: s.Pos()}
			}
			if s.Label != nil && !inner {
				return r.exit(lp, s, before)
			}
		case token.GOTO:
			if !inner {
				return r.exit(lp, s, before)
			}
		}
	case *ast.ReturnStmt:
		return r.returnStmt(s, lp, before)
	case *ast.DeferStmt:
		r.failLoop(lp, s.Pos(), "defer in the body of a range over a function")
	case *ast.BlockStmt:
		s.List = r.body(s.List, lp, loops, breaks, before)
	case *ast.IfStmt:
		s.Body.List = r.body(s.Body.List, lp, loops, breaks, before)
		if s.Else != nil {
			s.Else = r.bodyStmt(s.Else, lp, loops, breaks, before)
		}
	case *ast.ForStmt:
		s.Body.List = r.body(s.Body.List, lp, loops+1, breaks+1, before)
	case *ast.RangeStmt:
		s.Body.List = r.body(s.Body.List, lp, loops+1, breaks+1, before)
	case *ast.SwitchStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			cc.Body = r.body(cc.Body, lp, loops, breaks+1, before)
		}
	case *ast.TypeSwitchStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			cc.Body = r.body(cc.Body, lp, loops, breaks+1, before)
		}
	case *ast.SelectStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CommClause)
			cc.Body = r.body(cc.Body, lp, loops, breaks+1, before)
		}
	case *ast.LabeledStmt:
		s.Stmt = r.bodyStmt(s.Stmt, lp, loops, breaks, before)
	}
	return s
}

func (r *rewriter) failLoop(lp *loop, pos token.Pos, what string) {
	if !lp.bad {
		r.fail(pos, what)
	}
	lp.bad = true
}

// returnStmt rewrites a return in a loop's body: its results stored for
// the return after the loop, which the body's literal ends.
func (r *rewriter) returnStmt(s *ast.ReturnStmt, lp *loop, before token.Pos) ast.Stmt {
	pos := s.Pos()
	fc := lp.fc
	r.retVar(lp, before)
	if !lp.returns {
		lp.returns = true
		if fc.named == nil {
			for k := range fc.sig.Results().Len() {
				h, _ := r.newVar(fmt.Sprintf("#r%d_%d", r.n, k), fc.sig.Results().At(k).Type(), before)
				lp.results = append(lp.results, h)
			}
		}
	}
	var list []ast.Stmt
	if len(s.Results) > 0 {
		holders := lp.results
		if fc.named != nil {
			holders = fc.named
		}
		var lhs []ast.Expr
		for _, h := range holders {
			lhs = append(lhs, r.use(h, pos))
		}
		list = append(list, r.assign(lhs, s.Results, pos))
	}
	list = append(list, r.setInt(lp.retVar, 1, pos), r.setInt(lp.state, 1, pos), r.ret([]ast.Expr{r.boolLit(false, pos)}, pos))
	return &ast.BlockStmt{Lbrace: pos, List: list, Rbrace: pos}
}

func (r *rewriter) retVar(lp *loop, before token.Pos) {
	if lp.retVar == nil {
		lp.retVar, _ = r.newVar(fmt.Sprintf("#ret%d", r.n), types.Typ[types.Int], before)
	}
}

// exit rewrites a break, continue, or goto out of a loop's body: it ends
// the body's literal with its code, and the same branch follows the call
// (rewritten in turn if it leaves the body of a loop around).
func (r *rewriter) exit(lp *loop, s *ast.BranchStmt, before token.Pos) ast.Stmt {
	pos := s.Pos()
	r.retVar(lp, before)
	label := &ast.Ident{NamePos: s.Label.NamePos, Name: s.Label.Name}
	if obj := r.info.Uses[s.Label]; obj != nil {
		r.info.Uses[label] = obj
	}
	lp.exits = append(lp.exits, &ast.BranchStmt{TokPos: pos, Tok: s.Tok, Label: label})
	code := int64(1 + len(lp.exits))
	return &ast.BlockStmt{Lbrace: pos, List: []ast.Stmt{r.setInt(lp.retVar, code, pos), r.setInt(lp.state, 1, pos),
		r.ret([]ast.Expr{r.boolLit(false, pos)}, pos)}, Rbrace: pos}
}

// next ends the body's literal for the next iteration.
func (r *rewriter) next(lp *loop, pos token.Pos) ast.Stmt {
	return &ast.BlockStmt{Lbrace: pos, List: []ast.Stmt{r.setInt(lp.state, 0, pos), r.ret([]ast.Expr{r.boolLit(true, pos)}, pos)}, Rbrace: pos}
}
