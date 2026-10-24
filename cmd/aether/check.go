package main

import (
	"go/ast"
	"go/token"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/gate"
	"github.com/stevenstank/aether/internal/ir"
	"github.com/stevenstank/aether/internal/lower"
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
//  6. lowering to IR and verifying it: internal (lower.Lower, ir.Verify)
//
// An empty result means the program is inside S1. Step 6 cannot fail for a
// correct compiler; running it on every accepted program means a lowering
// bug is reported as internal instead of the program being accepted.
func check(srcs []source) []diag.Diagnostic {
	_, ds := compile(srcs)
	return ds
}

// compile runs the steps of check and returns the verified IR of a program
// inside S1, or the diagnostics and no IR.
func compile(srcs []source) (*ir.Program, []diag.Diagnostic) {
	names := make([]string, len(srcs))
	for i, s := range srcs {
		names[i] = s.name
	}
	if ds := gate.Files(names); len(ds) > 0 {
		return nil, ds
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
		return nil, ds
	}
	for _, f := range files {
		ds = append(ds, gate.Precheck(fset, f)...)
	}
	if len(ds) > 0 {
		return nil, ds
	}
	pkg, ds := frontend.TypeCheck(fset, files)
	if len(ds) > 0 {
		return nil, ds
	}
	if ds := gate.Check(pkg); len(ds) > 0 {
		return nil, ds
	}
	prog, ds := lower.Lower(pkg)
	if len(ds) > 0 {
		return nil, ds
	}
	if ds := verify(prog, names[0]); len(ds) > 0 {
		return nil, ds
	}
	return prog, nil
}

// verify runs the IR verifier. Lowering must produce well-formed IR, so a
// violation is an internal error, reported against the source file.
func verify(prog *ir.Program, file string) []diag.Diagnostic {
	var ds []diag.Diagnostic
	for _, err := range ir.Verify(prog) {
		ds = append(ds, diag.Diagnostic{
			Class:   diag.Internal,
			Pos:     diag.Pos{File: file},
			Message: "IR verifier: " + err.Error(),
		})
	}
	return ds
}
