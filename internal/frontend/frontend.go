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

// TypeCheck type-checks files as one package and then runs the program-level
// checks. All errors are Invalid diagnostics. No importer is installed: import
// declarations must have been rejected before this point (D-002).
func TypeCheck(fset *token.FileSet, files []*ast.File) (*Package, []diag.Diagnostic) {
	if len(files) == 0 {
		return nil, []diag.Diagnostic{{Class: diag.Internal, Message: "TypeCheck called with no files"}}
	}
	var ds []diag.Diagnostic
	conf := types.Config{
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
	}
	// The package path of a main package is "main", as in gc.
	pkg, _ := conf.Check(files[0].Name.Name, fset, files, info)
	if len(ds) == 0 {
		// After type errors the package scope may be incomplete (for
		// example, "var main" is reported and left out of it), so the
		// program-level checks would give misleading results.
		ds = programChecks(fset, files, pkg)
	}
	return &Package{Fset: fset, Files: files, Types: pkg, Info: info}, ds
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
