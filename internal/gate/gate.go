// Package gate decides whether a Go program is inside Aether's supported
// subset, S2 (docs/SEMANTICS.md §Subset S2): S1 (§Subset S1, D-012) plus the
// functions of Phase 4 increment 4.2 (§Functions), the bool and control flow
// of increment 4.1 (§Control flow and bool), the multiple results of increment
// 4.3, the integer types of increment 4.4 (§Integer types), the division and
// shifts of increment 4.5, the package-level variables and constants of
// increment 4.6, and the switch statements of increment 4.7, extended by
// float32 and float64 (Phase 6 increment 6.1, §Floating point), string
// constants (increment 6.2, §String constants), arrays and structs
// (increment 6.3, §Arrays and structs), pointers (Phase 7 increment 7.1,
// §Pointers), slices (increment 7.2, §Slices), strings made at run time
// (increment 7.3), and methods (Phase 8 increment 8.1, §Methods).
//
// The gate is default-deny. A construct is accepted only if a rule below
// explicitly allows it; anything else is reported as an Unsupported
// diagnostic naming the construct. That go/parser or go/types accepts a
// program says nothing about whether Aether supports it (D-003).
//
// The gate runs in two parts, in the order fixed by D-003:
//
//   - Files and Precheck, before type checking, reject file-level constructs:
//     more than one file, a package other than main, imports, directive
//     comments, and an empty result list on main or init (which go/types
//     would wrongly report as an error; E12).
//   - Check, after successful type checking, walks every declaration,
//     statement, and expression of every function using types.Info.
package gate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
)

// Feature names used in Unsupported diagnostics. They are listed in
// docs/SEMANTICS.md §Subset S1 and are stable: tests refer to them.
const (
	FeatMultipleFiles = "multiple-files"
	FeatPackageName   = "package-name"
	FeatImport        = "import"
	FeatCgo           = "cgo"
	FeatDirective     = "directive"
	FeatPackageDecl   = "package-level-declaration"
	FeatInitFunc      = "init-function"
	FeatFuncDecl      = "function-declaration"
	FeatLocalType     = "local-type-declaration"
	FeatConstImplicit = "const-implicit-value"
	FeatBlank         = "blank-identifier"
	FeatNonIntType    = "non-int-type"
	FeatAssignTarget  = "assignment-target"
	FeatMultiValue    = "multi-value-assignment"
	FeatPrintlnArg    = "println-argument-type"
	FeatBuiltin       = "builtin"
	FeatCall          = "call"
	FeatConversion    = "conversion"
	FeatVariadic      = "variadic-call"
	FeatPredeclared   = "predeclared-identifier"
	FeatImagLit       = "imaginary-literal"
	FeatCompositeLit  = "composite-literal"
	FeatSelector      = "selector"
	FeatIndex         = "index-expression"
	FeatSlice         = "slice-expression"
	FeatTypeExpr      = "type-expression"
	FeatExpression    = "expression"
	FeatStatement     = "statement"
	FeatEmptyResults  = "empty-result-list"
	FeatNoBody        = "function-without-body"
	FeatFrameSize     = "frame-size"
	FeatLoopVarAddr   = "loop-variable-address"
	// Reported by monomorphization (internal/mono, D-045).
	FeatGenericInstance = "generic-instance"
	FeatGenericUnused   = "generic-uninstantiated"
)

// Operator features are "operator-" followed by a name from unaryFeature,
// for example "operator-receive", or "other".
const featOperatorPrefix = "operator-"

// Files rejects programs consisting of more than one source file.
func Files(names []string) []diag.Diagnostic {
	if len(names) <= 1 {
		return nil
	}
	return []diag.Diagnostic{unsupported(diag.Pos{File: names[1]}, FeatMultipleFiles,
		fmt.Sprintf("a program of %d source files", len(names)))}
}

// Precheck rejects the constructs that must be rejected before type checking
// (docs/ARCHITECTURE.md §2): those go/types cannot check without capabilities
// Aether lacks, and those it would wrongly report as errors. It must run on a
// file that parsed without errors.
func Precheck(fset *token.FileSet, f *ast.File) []diag.Diagnostic {
	var ds []diag.Diagnostic
	pos := func(p token.Pos) diag.Pos { return frontend.Position(fset, p) }
	if f.Name.Name != "main" {
		ds = append(ds, unsupported(pos(f.Name.Pos()), FeatPackageName,
			fmt.Sprintf("package %s (only package main)", f.Name.Name)))
	}
	for _, imp := range f.Imports {
		if imp.Path.Value == `"C"` {
			ds = append(ds, unsupported(pos(imp.Pos()), FeatCgo, `cgo (import "C")`))
			continue
		}
		ds = append(ds, unsupported(pos(imp.Pos()), FeatImport, "import "+imp.Path.Value))
	}
	for _, g := range f.Comments {
		for _, c := range g.List {
			if name, ok := directive(c.Text); ok {
				ds = append(ds, unsupported(pos(c.Pos()), FeatDirective, "directive "+name))
			}
		}
	}
	// go/types rejects "func main() ()" as having results, because it tests
	// the result list for nil rather than for emptiness; gc accepts it, as
	// the specification does (E12). Rejecting it here keeps valid Go from
	// being classified as invalid.
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil &&
			(fd.Name.Name == "main" || fd.Name.Name == "init") &&
			fd.Type.Results != nil && fd.Type.Results.NumFields() == 0 {
			ds = append(ds, unsupported(pos(fd.Type.Results.Pos()), FeatEmptyResults,
				"an empty result list on func "+fd.Name.Name))
		}
	}
	return ds
}

// directive reports whether a comment is one of the directive forms S1
// rejects: //go:..., // +build, //line, and /*line.
func directive(text string) (string, bool) {
	switch {
	case strings.HasPrefix(text, "//go:"):
		name, _, _ := strings.Cut(text[2:], " ")
		return "//" + name, true
	case strings.HasPrefix(text, "//line ") || strings.HasPrefix(text, "/*line "):
		return text[:6], true
	case strings.HasPrefix(text, "//") && strings.HasPrefix(strings.TrimLeft(text[2:], " \t"), "+build"):
		return "// +build", true
	}
	return "", false
}

// Check walks a type-checked package and reports every construct outside the
// subset.
// It must only be called on a package that type-checked without errors.
func Check(pkg *frontend.Package) []diag.Diagnostic {
	c := &checker{pkg: pkg, info: pkg.Info, forVars: map[*types.Var]bool{}}
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			c.topDecl(d)
		}
	}
	if len(c.ds) == 0 {
		c.initFrame()
	}
	return c.ds
}

type checker struct {
	pkg  *frontend.Package
	info *types.Info
	ds   []diag.Diagnostic
	// forVars are the variables declared in the init statements of the for
	// statements checked so far, whose addresses are unsupported (D-035).
	forVars map[*types.Var]bool
	// lits are the function literals being checked, innermost last (D-040).
	lits []*ast.FuncLit
}

func (c *checker) pos(p token.Pos) diag.Pos { return frontend.Position(c.pkg.Fset, p) }

func (c *checker) reject(n ast.Node, feature, what string) {
	c.ds = append(c.ds, unsupported(c.pos(n.Pos()), feature, what))
}

func (c *checker) internal(n ast.Node, format string, args ...any) {
	c.ds = append(c.ds, diag.Diagnostic{
		Class:   diag.Internal,
		Pos:     c.pos(n.Pos()),
		Message: "subset gate: " + fmt.Sprintf(format, args...),
	})
}

// featureNames is the registry of every feature name the gate may report.
// It is the single source of truth: unsupported refuses any other name, and
// a test compares the registry with the table in docs/SEMANTICS.md.
var featureNames = map[string]bool{}

func init() {
	for _, f := range []string{
		FeatMultipleFiles, FeatPackageName, FeatImport, FeatCgo, FeatDirective,
		FeatPackageDecl, FeatInitFunc, FeatFuncDecl, FeatLocalType,
		FeatConstImplicit, FeatBlank, FeatNonIntType, FeatAssignTarget,
		FeatMultiValue, FeatPrintlnArg, FeatBuiltin, FeatCall,
		FeatConversion, FeatVariadic, FeatPredeclared, FeatImagLit,
		FeatCompositeLit,
		FeatSelector, FeatIndex, FeatSlice,
		FeatTypeExpr, FeatExpression, FeatStatement, FeatEmptyResults, FeatNoBody, FeatFrameSize,
		FeatLoopVarAddr, FeatGenericInstance, FeatGenericUnused,
		// Statement kinds, named by the statement keyword (see stmt).
		"for-range", "select", "go",
		"defer", "label",
		token.GOTO.String(), token.FALLTHROUGH.String(),
	} {
		featureNames[f] = true
	}
	for _, name := range unaryOperatorNames {
		featureNames[featOperatorPrefix+name] = true
	}
	featureNames[featOperatorPrefix+"other"] = true
}

// Features returns every feature name the gate may report, sorted.
func Features() []string {
	names := make([]string, 0, len(featureNames))
	for f := range featureNames {
		names = append(names, f)
	}
	sort.Strings(names)
	return names
}

// Reject returns the Unsupported diagnostic for what, a construct of the
// registered feature, for the stages besides the gate that reject
// constructs (internal/mono, D-045).
func Reject(pos diag.Pos, feature, what string) diag.Diagnostic {
	return unsupported(pos, feature, what)
}

func unsupported(pos diag.Pos, feature, what string) diag.Diagnostic {
	if !featureNames[feature] {
		return diag.Diagnostic{
			Class:   diag.Internal,
			Pos:     pos,
			Message: fmt.Sprintf("subset gate: unregistered feature name %q for %s", feature, what),
		}
	}
	return diag.Diagnostic{
		Class:   diag.Unsupported,
		Pos:     pos,
		Message: what + " is not supported in subset S1",
		Feature: feature,
	}
}

func (c *checker) topDecl(d ast.Decl) {
	switch d := d.(type) {
	case *ast.GenDecl:
		if d.Tok == token.IMPORT {
			// Precheck rejects imports before type checking.
			c.internal(d, "import declaration reached the typed check")
			return
		}
		if d.Tok == token.TYPE {
			c.typeDecl(d, FeatPackageDecl)
			return
		}
		if d.Tok != token.VAR && d.Tok != token.CONST {
			c.reject(d, FeatPackageDecl, "package-level "+d.Tok.String()+" declaration")
			return
		}
		// Package-level variables and constants follow the rules of local
		// ones; their initialization order is go/types' InitOrder.
		c.declStmt(d)
	case *ast.FuncDecl:
		switch {
		case d.Recv != nil:
			c.method(d)
		case d.Name.Name == "init":
			c.reject(d, FeatInitFunc, "init function")
		case d.Name.Name == "_":
			// Several functions may be named _, and none can be called.
			c.reject(d.Name, FeatBlank, "function named _")
		default:
			c.funcDecl(d)
		}
	default:
		c.reject(d, FeatPackageDecl, fmt.Sprintf("declaration %T", d))
	}
}

// funcDecl checks a function declaration: main, or a function whose
// parameters and results are of supported types. Blank and unnamed
// parameters are allowed. The body is checked only if the signature
// is supported, so one problem yields one diagnostic.
func (c *checker) funcDecl(d *ast.FuncDecl) {
	name := d.Name.Name
	// go/types rejects a main with type parameters, parameters, or results,
	// so a well-typed main has none of them.
	if name == "main" && d.Recv == nil && (d.Type.TypeParams != nil || d.Type.Params.NumFields() != 0 || d.Type.Results.NumFields() != 0) {
		c.internal(d, "unexpected signature for main after type checking")
		return
	}
	if d.Type.TypeParams != nil {
		c.reject(d, FeatFuncDecl, "generic function "+name)
		return
	}
	// A declaration without a body is valid Go: it declares a function
	// implemented outside Go, such as in assembly (spec §Function
	// declarations). go/types accepts it; Aether does not support it.
	if d.Body == nil {
		c.reject(d, FeatNoBody, "function "+name+" without a body (implemented outside Go)")
		return
	}
	mark := len(c.ds)
	ok := true
	if d.Recv != nil {
		ok = c.typeExpr(d.Recv.List[0].Type)
	}
	for _, field := range d.Type.Params.List {
		ok = c.typeExpr(field.Type) && ok
	}
	// An empty result list, "func f() ()", means no result. Precheck has
	// rejected it on main and init.
	// Named results are variables of the function (D-044).
	if results := d.Type.Results; results != nil {
		for _, field := range results.List {
			ok = c.typeExpr(field.Type) && ok
		}
	}
	if len(c.ds) > mark || !ok {
		return
	}
	c.stmts(d.Body.List)
	if len(c.ds) == mark {
		if n := c.funcBytes(d.Type, d.Body); n > maxFrameObjects {
			c.reject(d.Name, FeatFrameSize, fmt.Sprintf("function %s, whose arrays and structs may need %d bytes of stack (more than %d)", name, n, maxFrameObjects))
		}
	}
}

// funcBytes returns frameBytes of a function's body plus the frame
// objects of the named array and struct results its type declares (D-044).
func (c *checker) funcBytes(ft *ast.FuncType, body *ast.BlockStmt) int64 {
	n := c.frameBytes(body)
	if ft.Results != nil {
		n += c.frameBytes(ft.Results)
	}
	return n
}

// maxFrameObjects bounds the stack a function's array and struct variables
// and temporaries may need, half the backend's limit on a frame, which
// leaves the other half for its scalar slots and values (D-034).
const maxFrameObjects = 1 << 29

// frameBytes returns an upper bound on the bytes of the memory objects
// lowering gives the code n in its function's frame (D-034): one for each
// array or struct variable n declares, each such result of a call, each
// composite literal not nested in another, which is filled in place, and
// each other expression of array or struct type evaluated as a whole value,
// which is copied into a temporary, as is a method's value receiver. A
// variable, field, or element that is
// assigned, selected from, indexed, or whose address is taken is used in
// place, but a literal or call selected from or indexed is not, a literal whose address is taken is on the heap, and the operand of
// a constant len is not evaluated. Each object may need up to 8 more bytes
// for its alignment.
func (c *checker) frameBytes(n ast.Node) int64 {
	var total int64
	add := func(t types.Type) {
		if isAggregate(t) {
			total += sizes.Sizeof(t) + 8
		}
	}
	inPlace := map[ast.Expr]bool{}
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			inPlace[n.X] = c.addressed(n.X)
			// A method's value receiver is copied, for a call (D-038) or a
			// method value (D-039).
			if sel := c.info.Selections[n]; sel != nil && sel.Kind() == types.MethodVal {
				add(sel.Obj().Type().(*types.Signature).Recv().Type())
			}
		case *ast.IndexExpr:
			inPlace[n.X] = c.addressed(n.X)
		case *ast.SliceExpr:
			inPlace[n.X] = true // an array, on the heap or global (D-036)
		case *ast.ParenExpr:
			inPlace[n.X] = inPlace[n]
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				inPlace[l] = true
			}
		case *ast.IncDecStmt:
			inPlace[n.X] = true
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				inPlace[n.X] = true // a variable, or a literal on the heap
			}
		case *ast.CompositeLit:
			if isMap(c.info.Types[n].Type) {
				break // its elements are values, stored by assignment (D-042)
			}
			for _, elt := range n.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					elt = kv.Value
				}
				if _, ok := ast.Unparen(elt).(*ast.CompositeLit); ok {
					inPlace[ast.Unparen(elt)] = true
				}
			}
		case *ast.CallExpr:
			if tv := c.info.Types[n]; tv.Value != nil {
				for _, a := range n.Args {
					inPlace[a] = true // len of an array: not evaluated
				}
			}

		case *ast.Ident:
			if v, ok := c.info.Defs[n].(*types.Var); ok {
				add(v.Type())
				return false
			}
		case *ast.CaseClause:
			// A type switch's variable in this clause, and the value
			// copied into it (D-041).
			if v, ok := c.info.Implicits[n].(*types.Var); ok {
				add(v.Type())
				add(v.Type())
			}
		}
		e, ok := n.(ast.Expr)
		if !ok || inPlace[e] {
			return true
		}
		tv, ok := c.info.Types[e]
		if !ok || tv.IsType() {
			return true
		}
		switch e := e.(type) {
		case *ast.ParenExpr:
			return true // counted as its operand
		case *ast.CallExpr:
			if c.info.Types[e.Fun].IsType() {
				return true // a conversion: counted as its operand
			}
			if tuple, ok := tv.Type.(*types.Tuple); ok {
				for v := range tuple.Variables() {
					add(v.Type())
				}
				return true
			}
		case *ast.IndexExpr:
			// v, ok := m[k]: v, a temporary (D-042).
			if tuple, ok := tv.Type.(*types.Tuple); ok {
				add(tuple.At(0).Type())
				return true
			}
		case *ast.TypeAssertExpr:
			// v, ok := x.(T): v and its zero value, each a temporary.
			if tuple, ok := tv.Type.(*types.Tuple); ok {
				add(tuple.At(0).Type())
				add(tuple.At(0).Type())
				return true
			}
			add(tv.Type) // the value, and its own copy
		}
		add(tv.Type)
		return true
	})
	return total
}

// addressed reports whether lowering uses x, the operand of a selector or
// index expression, in place: not if it is a composite literal, a call, a
// type assertion, or a map's element, whose value it evaluates into a
// temporary.
func (c *checker) addressed(x ast.Expr) bool {
	switch x := ast.Unparen(x).(type) {
	case *ast.CompositeLit, *ast.CallExpr, *ast.TypeAssertExpr:
		return false
	case *ast.IndexExpr:
		return !c.isMap(x.X)
	}
	return true
}

// FrameBytes is the bound frameBytes for the code n of pkg, a package Check
// accepted. TestFrameBound checks that it is never below the memory
// objects lowering gives n.
func FrameBytes(pkg *frontend.Package, n ast.Node) int64 {
	return (&checker{pkg: pkg, info: pkg.Info}).frameBytes(n)
}

// FuncBytes is the bound funcBytes of a function of pkg, of type ft and
// body body, as FrameBytes is of other code.
func FuncBytes(pkg *frontend.Package, ft *ast.FuncType, body *ast.BlockStmt) int64 {
	return (&checker{pkg: pkg, info: pkg.Info}).funcBytes(ft, body)
}

// initFrame rejects package initialization whose frame, holding the
// temporaries of every package-level initializer, may exceed
// maxFrameObjects, at the initializer that crosses it.
func (c *checker) initFrame() {
	var total int64
	for _, f := range c.pkg.Files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				for _, v := range spec.(*ast.ValueSpec).Values {
					total += c.frameBytes(v)
					if total > maxFrameObjects {
						c.reject(v, FeatFrameSize, fmt.Sprintf("package initialization, whose arrays and structs may need %d bytes of stack (more than %d)", total, maxFrameObjects))
						return
					}
				}
			}
		}
	}
}

func (c *checker) stmts(list []ast.Stmt) {
	for _, s := range list {
		c.stmt(s)
	}
}

func (c *checker) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.DeclStmt:
		c.declStmt(s.Decl.(*ast.GenDecl))
	case *ast.AssignStmt:
		c.assign(s)
	case *ast.IncDecStmt:
		c.target(s.X)
	case *ast.ExprStmt:
		c.exprStmt(s)
	case *ast.BlockStmt:
		c.stmts(s.List)
	case *ast.EmptyStmt:
		// Allowed.
	case *ast.IfStmt:
		if s.Init != nil {
			c.stmt(s.Init)
		}
		c.expr(s.Cond)
		c.stmts(s.Body.List)
		if s.Else != nil {
			c.stmt(s.Else)
		}
	case *ast.ForStmt:
		// go/types has checked that init and post are simple statements.
		// Go 1.22 gives each iteration its own copy of a variable declared
		// in init, which a pointer can observe (D-035).
		if a, ok := s.Init.(*ast.AssignStmt); ok && a.Tok == token.DEFINE {
			for _, l := range a.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					if v, ok := c.info.Defs[id].(*types.Var); ok {
						c.forVars[v] = true
					}
				}
			}
		}
		if s.Init != nil {
			c.stmt(s.Init)
		}
		if s.Cond != nil {
			c.expr(s.Cond)
		}
		if s.Post != nil {
			c.stmt(s.Post)
		}
		c.stmts(s.Body.List)
	case *ast.RangeStmt:
		if c.isChan(s.X) {
			c.mapRange(s) // a channel's elements, as a map's keys (D-048)
			return
		}
		if !c.isMap(s.X) {
			c.reject(s, "for-range", "for-range statement")
			return
		}
		c.mapRange(s) // D-042
	case *ast.SwitchStmt:
		// go/types has checked that each case value is comparable with the
		// tag, and converted untyped constants to the tag's type.
		if s.Init != nil {
			c.stmt(s.Init)
		}
		if s.Tag != nil {
			if tv, ok := c.info.Types[s.Tag]; ok && (isAggregate(tv.Type) || isInterface(tv.Type)) {
				c.reject(s.Tag, FeatNonIntType, fmt.Sprintf("switch on a value of type %s", tv.Type))
				return
			}
			c.expr(s.Tag)
		}
		for _, cc := range s.Body.List {
			cc := cc.(*ast.CaseClause)
			for _, e := range cc.List {
				if tv, ok := c.info.Types[e]; ok && isInterface(tv.Type) {
					c.reject(e, FeatNonIntType, fmt.Sprintf("case value of type %s", tv.Type))
					continue
				}
				c.expr(e)
			}
			c.stmts(cc.Body)
		}
	case *ast.TypeSwitchStmt:
		c.typeSwitch(s)
	case *ast.SelectStmt:
		c.reject(s, "select", "select statement")
	case *ast.GoStmt:
		c.goStmt(s) // D-048
	case *ast.DeferStmt:
		c.deferStmt(s)
	case *ast.ReturnStmt:
		// go/types has checked the results against the signature.
		if !c.multiValue(s.Results) {
			for _, r := range s.Results {
				c.expr(r)
			}
		}
	case *ast.BranchStmt:
		switch {
		case s.Tok == token.GOTO || s.Tok == token.FALLTHROUGH:
			c.reject(s, s.Tok.String(), s.Tok.String()+" statement")
		case s.Label != nil:
			c.reject(s, "label", "labeled "+s.Tok.String())
		}
		// An unlabeled break or continue: every enclosing statement that
		// could be its target other than for and switch is rejected.
	case *ast.LabeledStmt:
		c.reject(s, "label", "labeled statement")
	case *ast.SendStmt:
		// go/types has checked that the value is assignable to the
		// element type (D-048).
		c.expr(s.Chan)
		c.expr(s.Value)
	default:
		c.reject(s, FeatStatement, fmt.Sprintf("statement %T", s))
	}
}

func (c *checker) declStmt(d *ast.GenDecl) {
	switch d.Tok {
	case token.VAR, token.CONST:
		for _, spec := range d.Specs {
			c.valueSpec(d.Tok, spec.(*ast.ValueSpec))
		}
	case token.TYPE:
		c.typeDecl(d, FeatLocalType)
	default:
		c.reject(d, FeatStatement, d.Tok.String()+" declaration")
	}
}

func (c *checker) valueSpec(tok token.Token, spec *ast.ValueSpec) {
	if tok == token.CONST && len(spec.Values) == 0 {
		// The implicit repetition of the previous expression in a
		// constant group is not part of S1.
		c.reject(spec, FeatConstImplicit, "constant declaration without a value")
		return
	}
	multi := len(spec.Names) > 1 && c.multiValue(spec.Values)
	if tok == token.VAR && !multi && len(spec.Values) != 0 && len(spec.Values) != len(spec.Names) {
		c.reject(spec, FeatMultiValue, "multi-value initialization")
		return
	}
	mark := len(c.ds)
	typeOK := spec.Type == nil || c.typeExpr(spec.Type)
	if !multi {
		for _, v := range spec.Values {
			c.expr(v)
		}
	}
	// A declared name's type follows from its type or initializer. Report it
	// only if those were not already reported, to avoid restating one
	// problem several times.
	explained := len(c.ds) > mark || !typeOK
	for _, name := range spec.Names {
		if name.Name == "_" {
			c.reject(name, FeatBlank, "the blank identifier")
			continue
		}
		obj := c.info.Defs[name]
		if obj == nil {
			c.internal(name, "no object for declared name %s", name.Name)
			continue
		}
		ok := c.supported(obj.Type()) || tok == token.CONST && c.supportedOrUntyped(obj.Type())
		if !ok && !explained {
			c.reject(name, FeatNonIntType, fmt.Sprintf("%s %s of type %s", tok, name.Name, obj.Type()))
		}
	}
}

// supportedTypeNames are the predeclared types the subset allows: bool,
// every integer type, with the aliases byte (uint8) and rune (int32), and
// float32 and float64 (Phase 6 increment 6.1).
var supportedTypeNames = []string{
	"bool", "int", "int8", "int16", "int32", "int64",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune",
	"float32", "float64", "string", "any", "error",
}

// typeExpr allows the predeclared types in supportedTypeNames, identified by
// object identity so that a shadowed or redefined name cannot stand in for
// them, the array and struct types of the program's type declarations,
// array and struct type literals of supported types (§Arrays and structs),
// and pointer types to supported types (§Pointers). It reports whether it
// allowed e.
func (c *checker) typeExpr(e ast.Expr) bool {
	switch t := ast.Unparen(e).(type) {
	case *ast.Ident:
		for _, name := range supportedTypeNames {
			if c.info.Uses[t] == types.Universe.Lookup(name) {
				return true
			}
		}
		if tn, ok := c.info.Uses[t].(*types.TypeName); ok && tn.Pkg() == c.pkg.Types && tn.Parent() != types.Universe {
			// A type declaration of the program, checked where it is: one
			// it rejects is not reported again at each use.
			return c.supported(tn.Type())
		}
	case *ast.ArrayType:
		if t.Len == nil {
			// A slice type (D-036).
			return c.typeExpr(t.Elt) && c.sized(e)
		}
		mark := len(c.ds)
		if _, ok := t.Len.(*ast.Ellipsis); !ok {
			c.expr(t.Len)
		}
		if len(c.ds) == mark && c.typeExpr(t.Elt) {
			return c.sized(e)
		}
		return false
	case *ast.StructType:
		mark := len(c.ds)
		for _, f := range t.Fields.List {
			c.typeExpr(f.Type)
		}
		if len(c.ds) == mark {
			return c.sized(e)
		}
		return false
	case *ast.StarExpr:
		return c.typeExpr(t.X) && c.sized(e)
	case *ast.ChanType:
		// A channel type (D-048).
		return c.typeExpr(t.Value) && c.sized(e)
	case *ast.MapType:
		// A map type (D-042).
		mark := len(c.ds)
		c.typeExpr(t.Key)
		c.typeExpr(t.Value)
		return len(c.ds) == mark && c.sized(e)
	case *ast.InterfaceType:
		return c.ifaceTypeExpr(t) && c.sized(e)
	case *ast.FuncType:
		// A func type (D-039): its parameter and result types, of which a
		// variadic ...T is rejected.
		if t.TypeParams != nil {
			break
		}
		mark := len(c.ds)
		for _, fl := range []*ast.FieldList{t.Params, t.Results} {
			if fl == nil {
				continue
			}
			for _, f := range fl.List {
				c.typeExpr(f.Type)
			}
		}
		if len(c.ds) == mark {
			return c.sized(e)
		}
		return false
	}
	c.reject(e, FeatNonIntType, "type "+types.ExprString(e))
	return false
}

// maxAggregate bounds the size of an array or struct type, so that the
// objects a function needs for its variables and temporaries fit its frame
// (D-034).
const maxAggregate = 1 << 24

// sized reports whether the array or struct type of e, or the type a
// pointer type e points to, is at most maxAggregate bytes, rejecting it if
// not.
func (c *checker) sized(e ast.Expr) bool {
	tv, ok := c.info.Types[e]
	if !ok {
		c.internal(e, "no type for type expression %s", types.ExprString(e))
		return false
	}
	if !c.supported(tv.Type) {
		if !isScalar(tv.Type) && !c.composite(tv.Type, map[*types.Named]bool{}) {
			c.reject(e, FeatNonIntType, "type "+types.ExprString(e))
			return false
		}
		c.reject(e, FeatNonIntType, fmt.Sprintf("type %s (larger than %d bytes)", types.ExprString(e), maxAggregate))
		return false
	}
	return true
}

// typeDecl checks a type declaration: only a defined array or struct type,
// not generic and not an alias, is allowed. feature names a rejected one.
func (c *checker) typeDecl(d *ast.GenDecl, feature string) {
	for _, spec := range d.Specs {
		spec := spec.(*ast.TypeSpec)
		tn, ok := c.info.Defs[spec.Name].(*types.TypeName)
		switch {
		case spec.Name.Name == "_":
			c.reject(spec.Name, FeatBlank, "type named _")
			continue
		case !ok:
			c.internal(spec, "no type name for %s", spec.Name.Name)
			continue
		}
		switch {
		case spec.Assign.IsValid():
			c.reject(spec, feature, "type alias "+spec.Name.Name)
		case spec.TypeParams != nil:
			c.reject(spec, feature, "generic type "+spec.Name.Name)
		case !isAggregate(tn.Type()) && !isInterface(tn.Type()):
			c.reject(spec, feature, fmt.Sprintf("type %s of underlying type %s (only arrays, structs, and interfaces)", spec.Name.Name, tn.Type().Underlying()))
		default:
			c.typeExpr(spec.Type)
		}
	}
}

func (c *checker) assign(s *ast.AssignStmt) {
	switch s.Tok {
	case token.DEFINE:
		multi := len(s.Lhs) > 1 && c.multiValue(s.Rhs)
		if !multi && len(s.Lhs) != len(s.Rhs) {
			c.reject(s, FeatMultiValue, "multi-value assignment")
			return
		}
		mark := len(c.ds)
		if !multi {
			for _, r := range s.Rhs {
				c.expr(r)
			}
		}
		explained := len(c.ds) > mark
		for _, l := range s.Lhs {
			id, ok := l.(*ast.Ident)
			if !ok {
				c.internal(l, "non-identifier on the left of :=")
				continue
			}
			if id.Name == "_" {
				c.reject(id, FeatBlank, "the blank identifier")
				continue
			}
			// A new variable is in Defs. A redeclared one is in Uses and
			// is assigned, not declared (E8, case S1-10).
			obj := c.info.Defs[id]
			if obj == nil {
				obj = c.info.Uses[id]
			}
			c.intVar(id, obj, !explained)
		}
		return
	case token.ASSIGN:
		multi := len(s.Lhs) > 1 && c.multiValue(s.Rhs)
		if !multi && len(s.Lhs) != len(s.Rhs) {
			c.reject(s, FeatMultiValue, "multi-value assignment")
			return
		}
		for _, l := range s.Lhs {
			c.target(l)
		}
		if multi {
			return
		}
	case token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN, token.QUO_ASSIGN, token.REM_ASSIGN,
		token.AND_ASSIGN, token.OR_ASSIGN, token.XOR_ASSIGN, token.AND_NOT_ASSIGN, token.SHL_ASSIGN, token.SHR_ASSIGN:
		// += of a string concatenates (D-037); go/types allows no other
		// operator on strings.
		c.target(s.Lhs[0])
	default:
		c.reject(s, featOperatorPrefix+"other", "compound assignment "+s.Tok.String())
		return
	}
	for _, r := range s.Rhs {
		c.expr(r)
	}
}

// target checks the left-hand side of an assignment or increment, or the
// operand of &: the name of a variable of the program, *p, or a field or
// array element of one of these, selected through any number of fields and
// elements (§Arrays and structs), and of pointers to structs and arrays
// (§Pointers). go/types has checked that it is addressable.
func (c *checker) target(e ast.Expr) {
	switch t := e.(type) {
	case *ast.StarExpr:
		c.expr(t.X)
		return
	case *ast.SelectorExpr:
		if sel := c.info.Selections[t]; sel != nil && sel.Kind() == types.FieldVal {
			c.operand(t.X, sel.Indirect())
			return
		}
	case *ast.IndexExpr:
		if c.isArray(t.X) || c.isArrayPtr(t.X) || c.isSlice(t.X) || c.isMap(t.X) {
			c.operand(t.X, c.isArrayPtr(t.X) || c.isSlice(t.X) || c.isMap(t.X))
			c.expr(t.Index)
			return
		}
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		c.reject(e, FeatAssignTarget, "assignment to "+types.ExprString(e))
		return
	}
	if id.Name == "_" {
		c.reject(id, FeatBlank, "the blank identifier")
		return
	}
	if v, ok := c.info.Uses[id].(*types.Var); ok && !c.capture(id, v) {
		return
	}
	// Every variable is declared in the program, where its declaration has
	// been checked; an unsupported type was reported there.
	c.intVar(id, c.info.Uses[id], false)
}

// operand checks the operand x of a selector or index expression that
// target allows: if the selection goes through a pointer (indirect), any
// expression, whose value is the pointer or holds it, and otherwise a
// target, or *p in parentheses.
func (c *checker) operand(x ast.Expr, indirect bool) {
	if s, ok := ast.Unparen(x).(*ast.StarExpr); ok && !indirect {
		c.expr(s.X)
		return
	}
	if indirect {
		c.expr(x)
		return
	}
	c.target(x)
}

// address checks the operand of &: a composite literal, or a target other
// than a variable declared in a for statement's init (D-035), or a field or
// element of one.
func (c *checker) address(x ast.Expr) {
	x = ast.Unparen(x)
	if lit, ok := x.(*ast.CompositeLit); ok {
		c.expr(lit)
		return
	}
	if v := c.root(x); v != nil && c.forVars[v] {
		c.reject(x, FeatLoopVarAddr, "the address of "+types.ExprString(x)+", of a variable declared in a for statement's init")
		return
	}
	c.target(x)
}

// root returns the variable whose memory x, an operand of &, lies in, or
// nil if x is reached through a pointer.
func (c *checker) root(x ast.Expr) *types.Var {
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		v, _ := c.info.Uses[x].(*types.Var)
		return v
	case *ast.SelectorExpr:
		if sel := c.info.Selections[x]; sel != nil && sel.Kind() == types.FieldVal && !sel.Indirect() {
			return c.root(x.X)
		}
	case *ast.IndexExpr:
		if c.isArray(x.X) {
			return c.root(x.X)
		}
	}
	return nil
}

// intVar checks that obj, named by id, is a variable of the program. If reportType is
// set, it also reports a type other than int.
func (c *checker) intVar(id *ast.Ident, obj types.Object, reportType bool) {
	v, ok := obj.(*types.Var)
	if !ok {
		c.internal(id, "%s is not a variable", id.Name)
		return
	}
	if !c.declared(id, v) {
		return
	}
	if reportType && !c.supported(v.Type()) {
		c.reject(id, FeatNonIntType, fmt.Sprintf("variable %s of type %s", id.Name, v.Type()))
	}
}

func (c *checker) exprStmt(s *ast.ExprStmt) {
	if isRecv(s.X) {
		c.expr(s.X) // a receive whose value is dropped (D-048)
		return
	}
	call, ok := s.X.(*ast.CallExpr)
	if !ok {
		c.reject(s, FeatStatement, "expression statement")
		return
	}
	name := "println"
	if c.isBuiltin(call.Fun, "print") {
		name = "print"
	} else if !c.isBuiltin(call.Fun, "println") {
		c.call(call)
		return
	}
	if call.Ellipsis.IsValid() {
		c.reject(call, FeatVariadic, "variadic call")
		return
	}
	// println(f()) prints every result of f, which go/types allows of any
	// type, and gc only of a type print can print.
	if c.multiValue(call.Args) {
		for v := range c.info.Types[call.Args[0]].Type.(*types.Tuple).Variables() {
			if !isScalar(v.Type()) {
				c.reject(call.Args[0], FeatPrintlnArg, fmt.Sprintf("%s argument of type %s", name, v.Type()))
				return
			}
		}
		return
	}
	for _, a := range call.Args {
		tv, ok := c.info.Types[a]
		if !ok {
			c.internal(a, "no type for println argument")
			continue
		}
		if !isScalar(tv.Type) {
			c.reject(a, FeatPrintlnArg, fmt.Sprintf("%s argument of type %s", name, tv.Type))
			continue
		}
		c.expr(a)
	}
}

// isBuiltin reports whether e is the plain identifier of the predeclared
// builtin function name, identified by object rather than by spelling.
func (c *checker) isBuiltin(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := c.info.Uses[id].(*types.Builtin)
	return ok && b.Name() == name
}

// call checks a call other than the print and println statements handled
// by exprStmt. Only direct calls of the program's functions and
// conversions to a supported type are allowed; it reports whether the call
// was. Every other call is rejected by kind.
func (c *checker) call(call *ast.CallExpr) bool {
	if tv, ok := c.info.Types[call.Fun]; ok && tv.IsType() {
		// go/types has checked that the conversion is valid, so a
		// supported target type other than string has a numeric or bool
		// operand. A string converts to and from a string, a []byte, and a
		// []rune, and from an integer (D-037).
		at := c.info.Types[call.Args[0]]
		switch {
		case isInterface(tv.Type):
			// To an interface (D-041): any supported value.
		case isString(tv.Type) && (isString(at.Type) || isByteSlice(at.Type) || isRuneSlice(at.Type) || isInteger(at.Type)),
			(isByteSlice(tv.Type) || isRuneSlice(tv.Type)) && isString(at.Type):
		case isString(tv.Type) || isString(at.Type):
			c.reject(call, FeatConversion, "conversion to "+types.ExprString(call.Fun))
			return false
		case isSlice(tv.Type) != isSlice(at.Type) && !at.IsNil():
			// A slice converts only to a slice type (D-036): not to an
			// array or a pointer to one.
			c.reject(call, FeatConversion, "conversion to "+types.ExprString(call.Fun))
			return false
		}
		mark := len(c.ds)
		if !c.typeExpr(call.Fun) {
			c.ds = c.ds[:mark]
			c.reject(call, FeatConversion, "conversion to "+types.ExprString(call.Fun))
			return false
		}
		c.expr(call.Args[0])
		return true
	}
	if se, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		// A method of the program's (D-038): the gate allows no other
		// type with methods.
		if sel := c.info.Selections[se]; sel != nil && sel.Kind() == types.MethodVal {
			if fn, ok := sel.Obj().(*types.Func); ok && (fn.Pkg() == c.pkg.Types || isInterface(sel.Recv())) {
				return c.methodCall(call, se.X, sel)
			}
		}
	}
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		switch obj := c.info.Uses[id].(type) {
		case *types.Builtin:
			// len of a string, its length in bytes (D-033), or of an array
			// or a pointer to one.
			// len and cap of a slice, cap of an array or a pointer to one.
			if (obj.Name() == "len" || obj.Name() == "cap") && len(call.Args) == 1 && !call.Ellipsis.IsValid() &&
				(obj.Name() == "len" && (c.isString(call.Args[0]) || c.isMap(call.Args[0])) || c.isArray(call.Args[0]) || c.isArrayPtr(call.Args[0]) || c.isSlice(call.Args[0])) {
				c.expr(call.Args[0])
				return true
			}
			if c.sliceBuiltin(call, obj.Name()) || c.mapBuiltin(call, obj.Name()) || c.chanBuiltin(call, obj.Name()) {
				return true
			}
			// new(T), but not new of a value, new(expr), which Go 1.26
			// added.
			if obj.Name() == "new" && len(call.Args) == 1 && c.info.Types[call.Args[0]].IsType() {
				return c.typeExpr(call.Args[0])
			}
			// recover(), of type any (D-043).
			if obj.Name() == "recover" && len(call.Args) == 0 {
				return true
			}
			// panic(v), of any supported operand, converted to any (D-043).
			if obj.Name() == "panic" && len(call.Args) == 1 && !call.Ellipsis.IsValid() {
				if !c.multiValue(call.Args) {
					c.expr(call.Args[0])
				}
				return true
			}
			c.reject(call, FeatBuiltin, "builtin "+obj.Name())
			return false
		case *types.Func:
			if obj.Parent() == c.pkg.Types.Scope() {
				// A function with an unsupported signature is rejected
				// at its declaration.
				if call.Ellipsis.IsValid() {
					c.reject(call, FeatVariadic, "variadic call")
					return false
				}
				if !c.multiValue(call.Args) {
					for _, a := range call.Args {
						c.expr(a)
					}
				}
				return true
			}
		}
	}
	if tv, ok := c.info.Types[call.Fun]; ok && !tv.IsType() && !tv.IsBuiltin() {
		if sig, ok := tv.Type.Underlying().(*types.Signature); ok {
			// A call of a func value (D-039), whose type, when its value is
			// checked, must be supported.
			if call.Ellipsis.IsValid() || sig.Variadic() {
				c.reject(call, FeatVariadic, "variadic call")
				return false
			}
			mark := len(c.ds)
			c.expr(call.Fun)
			if len(c.ds) > mark {
				return false
			}
			if !c.multiValue(call.Args) {
				for _, a := range call.Args {
					c.expr(a)
				}
			}
			return true
		}
	}
	c.reject(call, FeatCall, "function call")
	return false
}

// multiValue reports whether list is a single call with several results,
// used where Go allows one: as all the values of a return, of an
// assignment or declaration of as many names, or of the arguments of a
// call. It checks the call if so.
func (c *checker) multiValue(list []ast.Expr) bool {
	if len(list) != 1 {
		return false
	}
	if ta, ok := ast.Unparen(list[0]).(*ast.TypeAssertExpr); ok && isTuple(c.info.Types[list[0]].Type) {
		c.typeAssert(ta) // v, ok := x.(T) (D-041)
		return true
	}
	if ix, ok := ast.Unparen(list[0]).(*ast.IndexExpr); ok && isTuple(c.info.Types[list[0]].Type) && c.isMap(ix.X) {
		c.expr(ix.X) // v, ok := m[k] (D-042)
		c.expr(ix.Index)
		return true
	}
	if u, ok := ast.Unparen(list[0]).(*ast.UnaryExpr); ok && u.Op == token.ARROW && isTuple(c.info.Types[list[0]].Type) {
		c.expr(u.X) // v, ok := <-ch (D-048)
		return true
	}
	call, ok := list[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	if tv, ok := c.info.Types[call]; !ok || !isTuple(tv.Type) {
		return false
	}
	c.call(call)
	return true
}

func isTuple(t types.Type) bool {
	_, ok := t.(*types.Tuple)
	return ok
}

// expr checks an expression used as a value. Allowed forms are integer
// literals, true and false, variables and constants, binary + - *, the
// comparisons, && and ||, unary + - !, parentheses, and calls of the
// program's functions, and every allowed node must have type int or bool,
// typed or untyped. go/types has checked that each operator applies to its
// operands. The same rules apply inside constant expressions (D-012).
//
// A disallowed form is reported by kind and not descended into. An allowed
// form of another type is reported once, also without descending, so one
// problem yields one diagnostic.
func (c *checker) expr(e ast.Expr) {
	switch e := e.(type) {
	case *ast.BasicLit:
		switch e.Kind {
		case token.INT, token.FLOAT, token.STRING, token.CHAR:
			c.typed(e)
		case token.IMAG:
			c.reject(e, FeatImagLit, "imaginary literal "+e.Value)
			return
		default:
			c.internal(e, "unknown literal kind %s", e.Kind)
			return
		}
	case *ast.Ident:
		if c.valueIdent(e) {
			c.typed(e)
		}
	case *ast.ParenExpr:
		if c.typed(e) {
			c.expr(e.X)
		}
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			if c.typed(e) {
				c.address(e.X)
			}
			return
		}
		if e.Op == token.ARROW {
			if c.typed(e) {
				c.expr(e.X) // a receive (D-048)
			}
			return
		}
		if e.Op != token.ADD && e.Op != token.SUB && e.Op != token.NOT && e.Op != token.XOR {
			c.reject(e, unaryFeature(e.Op), "unary operator "+e.Op.String())
			return
		}
		if c.typed(e) {
			c.expr(e.X)
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.ADD, token.SUB, token.MUL, token.QUO, token.REM, token.LAND, token.LOR,
			token.AND, token.OR, token.XOR, token.AND_NOT, token.SHL, token.SHR,
			token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		default:
			c.reject(e, featOperatorPrefix+"other", "operator "+e.Op.String())
			return
		}
		if c.typed(e) {
			c.expr(e.X)
			c.expr(e.Y)
		}
	case *ast.CallExpr:
		if c.call(e) {
			c.typed(e)
		}
	case *ast.FuncLit:
		c.funcLit(e)
	case *ast.CompositeLit:
		// A literal of a supported array, struct, slice, or map type.
		if tv, ok := c.info.Types[e]; ok && !c.supported(tv.Type) {
			c.reject(e, FeatCompositeLit, fmt.Sprintf("composite literal of type %s", tv.Type))
			return
		}
		if c.typed(e) {
			c.compositeLit(e)
		}
	case *ast.SelectorExpr:
		// A field of an array or struct value, or of a pointer to a
		// struct, perhaps promoted through embedded structs and pointers
		// to them.
		sel := c.info.Selections[e]
		switch {
		case sel != nil && sel.Kind() == types.MethodVal:
			// Called, a method is checked by call; otherwise it is a
			// method value, a func value (D-039).
			c.methodValue(e, sel)
			return
		case sel != nil && sel.Kind() == types.MethodExpr:
			// A method expression T.M or (*T).M (D-039).
			if c.typeExpr(e.X) {
				c.typed(e)
			}
			return
		case sel == nil || sel.Kind() != types.FieldVal:
			c.reject(e, FeatSelector, "selector "+types.ExprString(e))
			return
		}
		if c.typed(e) {
			c.expr(e.X)
		}
	case *ast.IndexExpr:
		// Indexing a string gives one of its bytes (D-033), and indexing
		// an array, a pointer to one, or a slice one of its elements;
		// every other index expression is unsupported.
		if !c.isString(e.X) && !c.isArray(e.X) && !c.isArrayPtr(e.X) && !c.isSlice(e.X) && !c.isMap(e.X) {
			c.reject(e, FeatIndex, "index expression")
			return
		}
		if c.typed(e) {
			c.expr(e.X)
			c.expr(e.Index)
		}
	case *ast.IndexListExpr:
		c.reject(e, FeatIndex, "index expression")
	case *ast.SliceExpr:
		// Of a slice, a string (D-037), an addressable array, or a pointer
		// to an array.
		switch {
		case c.isSlice(e.X) || c.isString(e.X) || c.isArrayPtr(e.X):
			c.expr(e.X)
		case c.isArray(e.X):
			c.address(e.X)
		default:
			c.reject(e, FeatSlice, "slice expression")
			return
		}
		for _, b := range []ast.Expr{e.Low, e.High, e.Max} {
			if b != nil {
				c.expr(b)
			}
		}
		c.typed(e)
	case *ast.TypeAssertExpr:
		c.typeAssert(e)
	case *ast.StarExpr:
		// go/types has checked that e.X is a pointer: as a value, e is
		// not a type.
		if c.typed(e) {
			c.expr(e.X)
		}
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType, *ast.StructType:
		c.reject(e, FeatTypeExpr, "type expression")
	default:
		c.reject(e, FeatExpression, fmt.Sprintf("expression %T", e))
	}
}

// compositeLit checks an array or struct literal, whose type typed has
// allowed: its type as written, unless elided, the keys of an array literal,
// which are constants, and the elements, of which an elided literal is
// checked in turn. A struct literal's keys are field names.
func (c *checker) compositeLit(e *ast.CompositeLit) {
	if at, ok := e.Type.(*ast.ArrayType); ok && at.Len != nil {
		if _, ok := at.Len.(*ast.Ellipsis); ok {
			// [...]T: the length is the literal's.
			if !c.typeExpr(at.Elt) {
				return
			}
		} else if !c.typeExpr(e.Type) {
			return
		}
	} else if e.Type != nil && !c.typeExpr(e.Type) {
		return
	}
	// An array or slice literal's keys are constant indices, and a map
	// literal's are values, perhaps elided literals (D-042).
	array := isArray(c.info.Types[e].Type) || isSlice(c.info.Types[e].Type)
	for _, elt := range e.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if array {
				c.expr(kv.Key)
			} else if isMap(c.info.Types[e].Type) {
				c.element(kv.Key)
			}
			elt = kv.Value
		}
		c.element(elt)
	}
}

// element checks an element or map key of a composite literal: an elided
// literal is checked in turn.
func (c *checker) element(elt ast.Expr) {
	if lit, ok := elt.(*ast.CompositeLit); ok && lit.Type == nil {
		if c.typed(lit) {
			c.compositeLit(lit)
		}
		return
	}
	c.expr(elt)
}

// typed reports whether e has a supported type, typed or untyped,
// rejecting it if not.
func (c *checker) typed(e ast.Expr) bool {
	tv, ok := c.info.Types[e]
	if !ok {
		c.internal(e, "no type for expression %s", types.ExprString(e))
		return false
	}
	if !c.supportedOrUntyped(tv.Type) {
		c.reject(e, FeatNonIntType, fmt.Sprintf("expression of type %s", tv.Type))
		return false
	}
	return true
}

// valueIdent checks an identifier used as a value. It must denote a variable
// or constant declared in the program, inside a function or at package
// level. It reports whether the identifier is acceptable so far (its type
// is checked by the caller).
func (c *checker) valueIdent(id *ast.Ident) bool {
	obj := c.info.Uses[id]
	switch obj := obj.(type) {
	case *types.Var:
		return c.declared(id, obj) && c.capture(id, obj)
	case *types.Const:
		if obj == types.Universe.Lookup("true") || obj == types.Universe.Lookup("false") {
			return true
		}
		if obj.Parent() == types.Universe {
			c.reject(id, FeatPredeclared, "predeclared identifier "+id.Name)
			return false
		}
		return c.declared(id, obj)
	case *types.Nil:
		// Untyped nil stands for a value of the type its context gives it,
		// which the gate checks there: a pointer type is the only one
		// supported.
		return true
	case *types.TypeName:
		c.reject(id, FeatTypeExpr, "type "+id.Name)
	case *types.Builtin:
		c.reject(id, FeatBuiltin, "builtin "+id.Name)
	case *types.Func:
		// A function of the program as a value (D-039).
		if obj.Parent() == c.pkg.Types.Scope() {
			return true
		}
		c.reject(id, FeatCall, "function value "+id.Name)
	case nil:
		c.internal(id, "no object for identifier %s", id.Name)
	default:
		c.reject(id, FeatExpression, fmt.Sprintf("identifier %s (%T)", id.Name, obj))
	}
	return false
}

// declared reports whether obj, named by id, is declared in the program,
// inside a function or at package level, rather than predeclared.
func (c *checker) declared(id *ast.Ident, obj types.Object) bool {
	if obj.Parent() == types.Universe {
		c.internal(id, "%s is a predeclared variable", id.Name)
		return false
	}
	return true
}

var unaryOperatorNames = map[token.Token]string{
	token.ARROW: "receive",
}

func unaryFeature(op token.Token) string {
	if name, ok := unaryOperatorNames[op]; ok {
		return featOperatorPrefix + name
	}
	return featOperatorPrefix + "other"
}

// isScalar reports whether t is bool, an integer type, float32, float64, or
// string: a predeclared type of the subset. The complex types are not
// supported.
func isScalar(t types.Type) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	return ok && (b.Kind() == types.Bool || b.Kind() >= types.Int && b.Kind() <= types.Uintptr ||
		b.Kind() == types.Float32 || b.Kind() == types.Float64 || b.Kind() == types.String)
}

// sizes is the layout lowering gives arrays and structs (D-034).
var sizes = types.SizesFor("gc", "amd64")

// supported reports whether t is a scalar type, an array or struct type of
// at most maxAggregate bytes whose elements or fields are of supported
// types, or a pointer type to a supported type; a defined one must be a
// declaration of the program, not generic.
func (c *checker) supported(t types.Type) bool {
	return c.sizedType(t, map[*types.Named]bool{})
}

// sizedType is supported, where the defined types in seen have been or are
// being checked. Assuming they are supported is safe: each is a conjunct of
// the result, which is false if any of them is not. It ends on recursive
// types, which go/types allows only through pointers.
func (c *checker) sizedType(t types.Type, seen map[*types.Named]bool) bool {
	if isScalar(t) {
		return true
	}
	if !c.composite(t, seen) {
		return false
	}
	return sizes.Sizeof(t) <= maxAggregate
}

// composite reports whether t is an array or struct type whose elements or
// fields are of supported types, regardless of its size, or a pointer type
// to a supported type.
func (c *checker) composite(t types.Type, seen map[*types.Named]bool) bool {
	t = types.Unalias(t)
	if n, ok := t.(*types.Named); ok {
		if n == types.Universe.Lookup("error").Type() {
			return true // an interface (D-041)
		}
		if n.Obj().Pkg() != c.pkg.Types || n.Obj().Parent() == types.Universe || n.TypeParams().Len() > 0 || n.TypeArgs().Len() > 0 {
			return false
		}
		if seen[n] {
			return true
		}
		seen[n] = true
		t = n.Underlying()
	}
	switch t := t.(type) {
	case *types.Pointer:
		return c.sizedType(t.Elem(), seen)
	case *types.Slice:
		return c.sizedType(t.Elem(), seen)
	case *types.Map:
		// A map (D-042): go/types has checked that its key is comparable.
		return c.sizedType(t.Key(), seen) && c.sizedType(t.Elem(), seen)
	case *types.Chan:
		// A channel (D-048), of an element gc accepts.
		return c.sizedType(t.Elem(), seen) && sizes.Sizeof(t.Elem()) <= maxChanElem
	case *types.Array:
		return isScalar(t.Elem()) || c.composite(t.Elem(), seen)
	case *types.Struct:
		for f := range t.Fields() {
			if !isScalar(f.Type()) && !c.composite(f.Type(), seen) {
				return false
			}
			if f.Embedded() && isInterface(f.Type()) {
				return false // its methods would be promoted (D-041)
			}
		}
		return true
	case *types.Interface:
		return c.ifaceType(t, seen)
	case *types.Signature:
		// A func type (D-039), not variadic, of supported parameters and
		// results.
		if t.Variadic() || t.Recv() != nil || t.TypeParams().Len() > 0 {
			return false
		}
		for _, tuple := range []*types.Tuple{t.Params(), t.Results()} {
			for v := range tuple.Variables() {
				if !c.sizedType(v.Type(), seen) {
					return false
				}
			}
		}
		return true
	}
	return false
}

// isAggregate reports whether t is an array or struct type.
func isAggregate(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Array, *types.Struct:
		return true
	}
	return false
}

func isArray(t types.Type) bool {
	_, ok := t.Underlying().(*types.Array)
	return ok
}

// isArray reports whether the expression e has an array type.
func (c *checker) isArray(e ast.Expr) bool {
	tv, ok := c.info.Types[e]
	return ok && isArray(tv.Type)
}

func isSlice(t types.Type) bool {
	_, ok := t.Underlying().(*types.Slice)
	return ok
}

// isSlice reports whether the expression e has a slice type.
func (c *checker) isSlice(e ast.Expr) bool {
	tv, ok := c.info.Types[e]
	return ok && isSlice(tv.Type)
}

// sliceBuiltin checks a call of make, append, or copy on slices (D-036),
// reporting whether it is one: make of a slice type with a length and
// perhaps a capacity, append of values or of another slice, and copy
// between slices. Appending or copying a string's bytes is not supported.
func (c *checker) sliceBuiltin(call *ast.CallExpr, name string) bool {
	args := call.Args
	switch {
	case name == "make" && len(args) >= 2 && isSlice(c.info.Types[args[0]].Type) && !call.Ellipsis.IsValid():
		if !c.typeExpr(args[0]) {
			return true // reported
		}
		args = args[1:]
	// go/types allows a string's bytes only where the slice is a []byte.
	case name == "append" && len(args) >= 1 && c.isSlice(args[0]) && (!call.Ellipsis.IsValid() || c.isSlice(args[1]) || c.isString(args[1])):
	case name == "copy" && len(args) == 2 && c.isSlice(args[0]) && (c.isSlice(args[1]) || c.isString(args[1])):
	default:
		return false
	}
	for _, a := range args {
		c.expr(a)
	}
	return true
}

// isArrayPtr reports whether the expression e has a pointer to array type.
func (c *checker) isArrayPtr(e ast.Expr) bool {
	tv, ok := c.info.Types[e]
	if !ok {
		return false
	}
	p, ok := tv.Type.Underlying().(*types.Pointer)
	return ok && isArray(p.Elem())
}

// isByteSlice reports whether t is a slice of bytes.
func isByteSlice(t types.Type) bool {
	s, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	b, ok := s.Elem().Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

// isRuneSlice reports whether t is a slice of runes.
func isRuneSlice(t types.Type) bool {
	s, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	b, ok := s.Elem().Underlying().(*types.Basic)
	return ok && b.Kind() == types.Int32
}

// isInteger reports whether t is an integer type, typed or untyped.
func isInteger(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

// isString reports whether t is string or untyped string.
func isString(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && (b.Kind() == types.String || b.Kind() == types.UntypedString)
}

// isString reports whether the expression e has a string type.
func (c *checker) isString(e ast.Expr) bool {
	tv, ok := c.info.Types[e]
	return ok && isString(tv.Type)
}

// supportedOrUntyped reports whether t is a supported type, typed or
// untyped. Untyped nil is checked where it is used (valueIdent). An
// untyped complex constant can only come from an imaginary literal, which
// is rejected as such.
func (c *checker) supportedOrUntyped(t types.Type) bool {
	return c.supported(t) || types.Identical(t, types.Typ[types.UntypedNil]) || types.Identical(t, types.Typ[types.UntypedInt]) ||
		types.Identical(t, types.Typ[types.UntypedBool]) || types.Identical(t, types.Typ[types.UntypedRune]) ||
		types.Identical(t, types.Typ[types.UntypedFloat]) || types.Identical(t, types.Typ[types.UntypedString])
}
