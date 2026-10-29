// Package gate decides whether a Go program is inside Aether's supported
// subset: S1 (docs/SEMANTICS.md §Subset S1, D-012) plus the functions of
// Phase 4 increment 4.2 (§Functions), the bool and control flow of
// increment 4.1 (§Control flow and bool), the multiple results of increment
// 4.3, and the integer types of increment 4.4 (§Integer types).
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
	FeatMethod        = "method"
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
	FeatFloatLit      = "float-literal"
	FeatImagLit       = "imaginary-literal"
	FeatRuneLit       = "rune-literal"
	FeatStringLit     = "string-literal"
	FeatFuncLit       = "function-literal"
	FeatCompositeLit  = "composite-literal"
	FeatSelector      = "selector"
	FeatIndex         = "index-expression"
	FeatSlice         = "slice-expression"
	FeatTypeAssert    = "type-assertion"
	FeatDeref         = "pointer-dereference"
	FeatTypeExpr      = "type-expression"
	FeatExpression    = "expression"
	FeatStatement     = "statement"
	FeatEmptyResults  = "empty-result-list"
	FeatNoBody        = "function-without-body"
)

// Operator features are "operator-" followed by a name from operatorNames
// or unaryFeature, for example "operator-div".
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

// Check walks a type-checked package and reports every construct outside S1.
// It must only be called on a package that type-checked without errors.
func Check(pkg *frontend.Package) []diag.Diagnostic {
	c := &checker{pkg: pkg, info: pkg.Info}
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			c.topDecl(d)
		}
	}
	return c.ds
}

type checker struct {
	pkg  *frontend.Package
	info *types.Info
	ds   []diag.Diagnostic
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
		FeatPackageDecl, FeatInitFunc, FeatFuncDecl, FeatMethod, FeatLocalType,
		FeatConstImplicit, FeatBlank, FeatNonIntType, FeatAssignTarget,
		FeatMultiValue, FeatPrintlnArg, FeatBuiltin, FeatCall,
		FeatConversion, FeatVariadic, FeatPredeclared, FeatFloatLit, FeatImagLit,
		FeatRuneLit, FeatStringLit, FeatFuncLit, FeatCompositeLit,
		FeatSelector, FeatIndex, FeatSlice, FeatTypeAssert, FeatDeref,
		FeatTypeExpr, FeatExpression, FeatStatement, FeatEmptyResults, FeatNoBody,
		// Statement kinds, named by the statement keyword (see stmt).
		"for-range", "switch", "type-switch", "select", "go",
		"defer", "label", "send",
		token.GOTO.String(), token.FALLTHROUGH.String(),
	} {
		featureNames[f] = true
	}
	for _, name := range operatorNames {
		featureNames[featOperatorPrefix+name] = true
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
		c.reject(d, FeatPackageDecl, "package-level "+d.Tok.String()+" declaration")
	case *ast.FuncDecl:
		switch {
		case d.Recv != nil:
			c.reject(d, FeatMethod, "method "+d.Name.Name)
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
// parameters and unnamed results are int or bool. Blank and unnamed
// parameters are allowed. The body is checked only if the signature
// is supported, so one problem yields one diagnostic.
func (c *checker) funcDecl(d *ast.FuncDecl) {
	name := d.Name.Name
	// go/types rejects a main with type parameters, parameters, or results,
	// so a well-typed main has none of them.
	if name == "main" && (d.Type.TypeParams != nil || d.Type.Params.NumFields() != 0 || d.Type.Results.NumFields() != 0) {
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
	for _, field := range d.Type.Params.List {
		c.typeExpr(field.Type)
	}
	// An empty result list, "func f() ()", means no result. Precheck has
	// rejected it on main and init.
	if results := d.Type.Results; results.NumFields() > 0 {
		// Results are either all named or all unnamed.
		if len(results.List[0].Names) > 0 {
			c.reject(results, FeatFuncDecl, "function "+name+" with named results")
		} else {
			for _, field := range results.List {
				c.typeExpr(field.Type)
			}
		}
	}
	if len(c.ds) > mark {
		return
	}
	c.stmts(d.Body.List)
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
		c.reject(s, "for-range", "for-range statement")
	case *ast.SwitchStmt:
		c.reject(s, "switch", "switch statement")
	case *ast.TypeSwitchStmt:
		c.reject(s, "type-switch", "type switch statement")
	case *ast.SelectStmt:
		c.reject(s, "select", "select statement")
	case *ast.GoStmt:
		c.reject(s, "go", "go statement")
	case *ast.DeferStmt:
		c.reject(s, "defer", "defer statement")
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
		// could be its target other than for is rejected.
	case *ast.LabeledStmt:
		c.reject(s, "label", "labeled statement")
	case *ast.SendStmt:
		c.reject(s, "send", "channel send")
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
		c.reject(d, FeatLocalType, "local type declaration")
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
	if spec.Type != nil {
		c.typeExpr(spec.Type)
	}
	if !multi {
		for _, v := range spec.Values {
			c.expr(v)
		}
	}
	// A declared name's type follows from its type or initializer. Report it
	// only if those were not already reported, to avoid restating one
	// problem several times.
	explained := len(c.ds) > mark
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
		ok := isSupported(obj.Type()) || tok == token.CONST && isSupportedOrUntyped(obj.Type())
		if !ok && !explained {
			c.reject(name, FeatNonIntType, fmt.Sprintf("%s %s of type %s", tok, name.Name, obj.Type()))
		}
	}
}

// supportedTypeNames are the predeclared types the subset allows: bool and
// every integer type, with the aliases byte (uint8) and rune (int32).
var supportedTypeNames = []string{
	"bool", "int", "int8", "int16", "int32", "int64",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune",
}

// typeExpr allows exactly the predeclared types in supportedTypeNames,
// identified by object identity so that a shadowed or redefined name cannot
// stand in for them.
func (c *checker) typeExpr(e ast.Expr) {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok {
		for _, name := range supportedTypeNames {
			if c.info.Uses[id] == types.Universe.Lookup(name) {
				return
			}
		}
	}
	c.reject(e, FeatNonIntType, "type "+types.ExprString(e))
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
	case token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN,
		token.AND_ASSIGN, token.OR_ASSIGN, token.XOR_ASSIGN, token.AND_NOT_ASSIGN:
		c.target(s.Lhs[0])
	default:
		op := strings.TrimSuffix(s.Tok.String(), "=")
		c.reject(s, operatorFeature(op), "compound assignment "+s.Tok.String())
		return
	}
	for _, r := range s.Rhs {
		c.expr(r)
	}
}

// target checks the left-hand side of an assignment or increment: it must be
// the plain name of an S1 variable.
func (c *checker) target(e ast.Expr) {
	id, ok := e.(*ast.Ident)
	if !ok {
		c.reject(e, FeatAssignTarget, "assignment to "+types.ExprString(e))
		return
	}
	if id.Name == "_" {
		c.reject(id, FeatBlank, "the blank identifier")
		return
	}
	// Every S1 variable is declared inside a function, where its declaration has
	// already been checked; a non-int type was reported there.
	c.intVar(id, c.info.Uses[id], false)
}

// intVar checks that obj, named by id, is a local variable. If reportType is
// set, it also reports a type other than int.
func (c *checker) intVar(id *ast.Ident, obj types.Object, reportType bool) {
	v, ok := obj.(*types.Var)
	if !ok {
		c.internal(id, "%s is not a variable", id.Name)
		return
	}
	if !c.local(id, v) {
		return
	}
	if reportType && !isSupported(v.Type()) {
		c.reject(id, FeatNonIntType, fmt.Sprintf("variable %s of type %s", id.Name, v.Type()))
	}
}

func (c *checker) exprStmt(s *ast.ExprStmt) {
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
	// println(f()) prints every result of f, whose types its declaration
	// has checked.
	if c.multiValue(call.Args) {
		return
	}
	for _, a := range call.Args {
		tv, ok := c.info.Types[a]
		if !ok {
			c.internal(a, "no type for println argument")
			continue
		}
		if !isSupported(tv.Type) {
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
		// supported target type has an integer or bool operand.
		mark := len(c.ds)
		c.typeExpr(call.Fun)
		if len(c.ds) > mark {
			c.ds = c.ds[:mark]
			c.reject(call, FeatConversion, "conversion to "+types.ExprString(call.Fun))
			return false
		}
		c.expr(call.Args[0])
		return true
	}
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		switch obj := c.info.Uses[id].(type) {
		case *types.Builtin:
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
		case token.INT:
			c.typed(e)
		case token.FLOAT:
			c.reject(e, FeatFloatLit, "floating-point literal "+e.Value)
			return
		case token.IMAG:
			c.reject(e, FeatImagLit, "imaginary literal "+e.Value)
			return
		case token.CHAR:
			c.reject(e, FeatRuneLit, "rune literal "+e.Value)
			return
		case token.STRING:
			c.reject(e, FeatStringLit, "string literal")
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
		if e.Op != token.ADD && e.Op != token.SUB && e.Op != token.NOT && e.Op != token.XOR {
			c.reject(e, unaryFeature(e.Op), "unary operator "+e.Op.String())
			return
		}
		if c.typed(e) {
			c.expr(e.X)
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.ADD, token.SUB, token.MUL, token.LAND, token.LOR,
			token.AND, token.OR, token.XOR, token.AND_NOT,
			token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		default:
			c.reject(e, operatorFeature(e.Op.String()), "operator "+e.Op.String())
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
		c.reject(e, FeatFuncLit, "function literal")
	case *ast.CompositeLit:
		c.reject(e, FeatCompositeLit, "composite literal")
	case *ast.SelectorExpr:
		c.reject(e, FeatSelector, "selector "+types.ExprString(e))
	case *ast.IndexExpr, *ast.IndexListExpr:
		c.reject(e, FeatIndex, "index expression")
	case *ast.SliceExpr:
		c.reject(e, FeatSlice, "slice expression")
	case *ast.TypeAssertExpr:
		c.reject(e, FeatTypeAssert, "type assertion")
	case *ast.StarExpr:
		c.reject(e, FeatDeref, "pointer dereference")
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType, *ast.StructType:
		c.reject(e, FeatTypeExpr, "type expression")
	default:
		c.reject(e, FeatExpression, fmt.Sprintf("expression %T", e))
	}
}

// typed reports whether e has type int or bool, typed or untyped, rejecting
// it if not.
func (c *checker) typed(e ast.Expr) bool {
	tv, ok := c.info.Types[e]
	if !ok {
		c.internal(e, "no type for expression %s", types.ExprString(e))
		return false
	}
	if !isSupportedOrUntyped(tv.Type) {
		c.reject(e, FeatNonIntType, fmt.Sprintf("expression of type %s", tv.Type))
		return false
	}
	return true
}

// valueIdent checks an identifier used as a value. It must denote a variable
// or constant declared inside a function. It reports whether the identifier is
// acceptable so far (its type is checked by the caller).
func (c *checker) valueIdent(id *ast.Ident) bool {
	obj := c.info.Uses[id]
	switch obj := obj.(type) {
	case *types.Var:
		return c.local(id, obj)
	case *types.Const:
		if obj == types.Universe.Lookup("true") || obj == types.Universe.Lookup("false") {
			return true
		}
		if obj.Parent() == types.Universe {
			c.reject(id, FeatPredeclared, "predeclared identifier "+id.Name)
			return false
		}
		return c.local(id, obj)
	case *types.Nil:
		c.reject(id, FeatPredeclared, "predeclared identifier nil")
	case *types.TypeName:
		c.reject(id, FeatTypeExpr, "type "+id.Name)
	case *types.Builtin:
		c.reject(id, FeatBuiltin, "builtin "+id.Name)
	case *types.Func:
		c.reject(id, FeatCall, "function value "+id.Name)
	case nil:
		c.internal(id, "no object for identifier %s", id.Name)
	default:
		c.reject(id, FeatExpression, fmt.Sprintf("identifier %s (%T)", id.Name, obj))
	}
	return false
}

// local reports whether obj, named by id, is declared inside a function. A
// package-level object is reachable here because package-level declarations
// are rejected but still type-check; uses of one are rejected as well.
func (c *checker) local(id *ast.Ident, obj types.Object) bool {
	switch obj.Parent() {
	case c.pkg.Types.Scope():
		c.reject(id, FeatPackageDecl, "use of package-level "+id.Name)
		return false
	case types.Universe:
		c.internal(id, "%s is a predeclared variable", id.Name)
		return false
	}
	return true
}

var operatorNames = map[string]string{
	"/": "div", "%": "rem", "<<": "shl", ">>": "shr",
}

func operatorFeature(op string) string {
	if name, ok := operatorNames[op]; ok {
		return featOperatorPrefix + name
	}
	return featOperatorPrefix + "other"
}

var unaryOperatorNames = map[token.Token]string{
	token.AND: "address", token.ARROW: "receive",
}

func unaryFeature(op token.Token) string {
	if name, ok := unaryOperatorNames[op]; ok {
		return featOperatorPrefix + name
	}
	return featOperatorPrefix + "other"
}

// isSupported reports whether t is bool or an integer type. Only
// predeclared types reach here: type declarations are rejected.
func isSupported(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && (b.Kind() == types.Bool || b.Kind() >= types.Int && b.Kind() <= types.Uintptr)
}

// isSupportedOrUntyped reports whether t is bool or an integer type, typed
// or untyped. An untyped rune constant can only come from a rune literal,
// which is rejected as such.
func isSupportedOrUntyped(t types.Type) bool {
	return isSupported(t) || types.Identical(t, types.Typ[types.UntypedInt]) ||
		types.Identical(t, types.Typ[types.UntypedBool]) || types.Identical(t, types.Typ[types.UntypedRune])
}
