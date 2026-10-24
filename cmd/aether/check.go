package main

import (
	"go/ast"
	"go/token"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/gate"
)

type source struct {
	name string
	data []byte
}

// check classifies a program. Each step runs only if every earlier step
// reported nothing, which yields the precedence of D-003:
//
//  1. more than one file: unsupported (gate.Files)
//  2. syntax errors: invalid (a file must parse before it can be inspected)
//  3. file-level constructs: unsupported (gate.Precheck)
//  4. type errors and program-level checks: invalid (frontend.TypeCheck)
//  5. everything else outside S1: unsupported (gate.Check)
//
// An empty result means the program is inside S1.
func check(srcs []source) []diag.Diagnostic {
	names := make([]string, len(srcs))
	for i, s := range srcs {
		names[i] = s.name
	}
	if ds := gate.Files(names); len(ds) > 0 {
		return ds
	}
	fset := token.NewFileSet()
	var files []*ast.File
	var ds []diag.Diagnostic
	for _, s := range srcs {
		f, errs := frontend.Parse(fset, s.name, s.data)
		ds = append(ds, errs...)
		if f != nil {
			files = append(files, f)
		}
	}
	if len(ds) > 0 {
		return ds
	}
	for _, f := range files {
		ds = append(ds, gate.Precheck(fset, f)...)
	}
	if len(ds) > 0 {
		return ds
	}
	pkg, ds := frontend.TypeCheck(fset, files)
	if len(ds) > 0 {
		return ds
	}
	return gate.Check(pkg)
}
