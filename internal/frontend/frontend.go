// Package frontend parses and type-checks Go source for Aether.
//
// Parsing and type checking are not implemented by Aether: this package is a
// thin, explicitly configured wrapper around the standard library's go/parser
// and go/types (D-001, D-002). What it adds is the conversion of their errors
// into Aether diagnostics, and the program-level checks that gc performs
// outside its type checker and go/types therefore does not.
//
// Deciding whether Aether supports a program is not done here; that is the
// subset gate's job (package gate).
package frontend

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/stevenstank/aether/internal/diag"
)

// GoVersion is the Go language version Aether type-checks against (D-015).
const GoVersion = "go1.26"

// ParseMode keeps comments, which the subset gate needs in order to reject
// directives (D-001, E11), and skips the deprecated ast.Object resolution.
const ParseMode = parser.ParseComments | parser.SkipObjectResolution

// Package is a type-checked package: the input to the subset gate and, in
// later phases, to lowering.
type Package struct {
	Fset  *token.FileSet
	Files []*ast.File
	Types *types.Package
	Info  *types.Info
	// Importer resolves the package's imports, nil if it has none: the
	// loader (D-049). Monomorphization type-checks the package again with
	// it.
	Importer types.Importer
	// Others gives monomorphization the program's other packages, nil for
	// a package checked alone (D-052).
	Others Others
	// Homes maps the objects of instances this package makes of another
	// package's generic code to that package, whose names they have at
	// run time (D-052).
	Homes map[types.Object]*types.Package
}

// Others is the loader's view of a program's packages, for the instances
// a package makes of another package's generic code (D-052).
type Others interface {
	// Rank returns the place of the package path in initialization
	// order: a package ranks after those it imports.
	Rank(path string) int
	// Original returns the original check of the package path.
	Original(path string) *Package
	// Prepared returns the prepared package path, nil if it is not
	// prepared yet.
	Prepared(path string) *types.Package
	// Home returns the home of obj, a declaration of a prepared package,
	// if it has one (Package.Homes), or nil.
	Home(obj types.Object) *types.Package
}

// Parse parses one source file. Syntax errors are returned as Invalid
// diagnostics, in which case the returned file must not be used.
func Parse(fset *token.FileSet, filename string, src []byte) (*ast.File, []diag.Diagnostic) {
	f, err := parser.ParseFile(fset, filename, src, ParseMode)
	if err == nil {
		return f, nil
	}
	// go/parser positions syntax errors through //line directives and
	// reports only the adjusted position, which can name a file that does
	// not exist. Re-parsing with the directives disabled by a same-length
	// substitution leaves every offset unchanged, so it yields the same
	// errors at their real positions (D-018).
	if disabled, ok := disableLineDirectives(src); ok {
		if _, err2 := parser.ParseFile(token.NewFileSet(), filename, disabled, ParseMode); err2 != nil {
			err = err2
		}
	}
	var list scanner.ErrorList
	if !errors.As(err, &list) {
		return nil, []diag.Diagnostic{{
			Class:   diag.Internal,
			Pos:     diag.Pos{File: filename},
			Message: fmt.Sprintf("unexpected parser error: %v", err),
		}}
	}
	ds := make([]diag.Diagnostic, 0, len(list))
	for _, e := range list {
		ds = append(ds, diag.Diagnostic{
			Class:   diag.Invalid,
			Pos:     diag.Pos{File: e.Pos.Filename, Line: e.Pos.Line, Col: e.Pos.Column},
			Message: e.Msg,
		})
	}
	return nil, ds
}

// disableLineDirectives returns a copy of src in which "//line " and
// "/*line " no longer form line directives, and whether src had any.
func disableLineDirectives(src []byte) ([]byte, bool) {
	if !bytes.Contains(src, []byte("//line ")) && !bytes.Contains(src, []byte("/*line ")) {
		return nil, false
	}
	out := bytes.ReplaceAll(src, []byte("//line "), []byte("//xine "))
	return bytes.ReplaceAll(out, []byte("/*line "), []byte("/*xine ")), true
}

// TypeCheck type-checks files as one package without imports, whose path
// is its name, and then runs the program-level checks. All errors are
// Invalid diagnostics.
func TypeCheck(fset *token.FileSet, files []*ast.File) (*Package, []diag.Diagnostic) {
	if len(files) == 0 {
		return nil, []diag.Diagnostic{{Class: diag.Internal, Message: "TypeCheck called with no files"}}
	}
	return CheckPackage(fset, files[0].Name.Name, files, nil)
}

// CheckPackage type-checks files as the package with the import path path,
// whose imports imp resolves, and runs the program-level checks on main.
// The only importer is the loader (D-002, D-049).
func CheckPackage(fset *token.FileSet, path string, files []*ast.File, imp types.Importer) (*Package, []diag.Diagnostic) {
	if len(files) == 0 {
		return nil, []diag.Diagnostic{{Class: diag.Internal, Message: "TypeCheck called with no files"}}
	}
	var ds []diag.Diagnostic
	conf := types.Config{
		Importer:  imp,
		GoVersion: GoVersion,
		Sizes:     types.SizesFor("gc", "amd64"),
		Error: func(err error) {
			var terr types.Error
			if !errors.As(err, &terr) {
				ds = append(ds, diag.Diagnostic{
					Class:   diag.Internal,
					Message: fmt.Sprintf("unexpected type checker error: %v", err),
				})
				return
			}
			ds = append(ds, diag.Diagnostic{
				Class: diag.Invalid,
				Pos:   Position(fset, terr.Pos),
				// Secondary errors start with a tab (see types.Config.Error).
				Message: strings.TrimPrefix(terr.Msg, "\t"),
			})
		},
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Scopes:     map[ast.Node]*types.Scope{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	// The package path of a main package is "main", as in gc.
	pkg, _ := conf.Check(path, fset, files, info)
	if len(ds) == 0 && path == "main" {
		// After type errors the package scope may be incomplete (for
		// example, "var main" is reported and left out of it), so the
		// program-level checks would give misleading results.
		ds = programChecks(fset, files, pkg)
	}
	return &Package{Fset: fset, Files: files, Types: pkg, Info: info, Importer: imp}, ds
}

// programChecks reports violations of rules that the Go specification places
// on programs but go/types does not check, because gc enforces them outside
// its type checker. Experiment E8 found one relevant to S1: a main package
// must declare a function main. go/types already rejects a main that is not a
// function or has the wrong signature.
func programChecks(fset *token.FileSet, files []*ast.File, pkg *types.Package) []diag.Diagnostic {
	if pkg == nil || pkg.Name() != "main" || pkg.Scope().Lookup("main") != nil {
		return nil
	}
	return []diag.Diagnostic{{
		Class:   diag.Invalid,
		Pos:     Position(fset, files[0].Name.Pos()),
		Message: "function main is undeclared in the main package",
	}}
}

// Position converts a token.Pos to a diagnostic position. It uses the
// unadjusted position, so //line directives cannot redirect diagnostics to
// another file or line.
func Position(fset *token.FileSet, pos token.Pos) diag.Pos {
	if !pos.IsValid() {
		return diag.Pos{}
	}
	p := fset.PositionFor(pos, false)
	return diag.Pos{File: p.Filename, Line: p.Line, Col: p.Column}
}

// hostedMark separates, in the identifier of an instance a package hosts
// (D-052), the instance's name from its home package's path, so that it
// cannot collide with the host's own instance of a generic of the same
// name. No source can write it: "·" is not a letter.
const hostedMark = "·of·"

// HostedName returns the identifier of the hosted instance name of a
// generic of the package path.
func HostedName(name, path string) string { return name + hostedMark + path }

// SourceName returns name, an object's identifier, as the source names
// it: a hosted instance's without its home's path, and a hoisted local
// type's without its number (LocalName).
func SourceName(name string) string {
	if i := strings.Index(name, hostedMark); i >= 0 {
		return name[:i]
	}
	if i := strings.LastIndex(name, "·"); i > 0 && i+len("·") < len(name) &&
		strings.Trim(name[i+len("·"):], "0123456789") == "" {
		return name[:i]
	}
	return name
}

// LocalName returns the identifier of a type declared in a function that
// monomorphization moves to the package level, the package's n-th such
// declaration in source order: its name, "·", and n, as gc writes the
// type in the name of an instance it is a type argument of (D-057). No
// source can write it.
func LocalName(name string, n int) string { return name + "·" + strconv.Itoa(n) }
