package main

import (
	"go/token"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/gate"
	"github.com/stevenstank/aether/internal/ir"
	"github.com/stevenstank/aether/internal/lower"
	"github.com/stevenstank/aether/internal/mono"
)

type source struct {
	name string
	data []byte
}

// check classifies a program. Each step runs only if every earlier step
// reported nothing, which yields the precedence of D-003:
//
//  1. files in more than one directory: unsupported (gate.Files)
//  2. syntax errors: invalid (a file must parse before it can be inspected)
//  3. file-level constructs: unsupported (gate.Precheck), and imports the
//     loader cannot resolve
//  4. type errors and program-level checks: invalid (frontend.CheckPackage)
//  5. everything else outside the subset: unsupported (mono.Instantiate,
//     which replaces generic declarations by their instances, D-045, then
//     gate.Check on the instances)
//  6. lowering to IR and verifying it: internal (lower.LowerProgram,
//     ir.Verify)
//
// Steps 2 to 4 and mono.Instantiate run on each package, the packages it
// imports first, by frontend.Load (D-049); gate.Check then runs on each
// package in the order they are initialized.
//
// An empty result means the program is inside the subset (S2). Step 6 cannot fail for a
// correct compiler; running it on every accepted program means a lowering
// bug is reported as internal instead of the program being accepted.
func check(srcs []source) []diag.Diagnostic {
	_, ds := compile(srcs)
	return ds
}

// compile runs the steps of check and returns the verified IR of a program
// inside the subset, or the diagnostics and no IR.
func compile(srcs []source) (*ir.Program, []diag.Diagnostic) {
	names := make([]string, len(srcs))
	fsrcs := make([]frontend.Source, len(srcs))
	for i, s := range srcs {
		names[i] = s.name
		fsrcs[i] = frontend.Source{Name: s.name, Data: s.data}
	}
	if ds := gate.Files(names); len(ds) > 0 {
		return nil, ds
	}
	program, ds := frontend.Load(token.NewFileSet(), fsrcs, frontend.Stages{Precheck: gate.Precheck, Prepare: mono.Instantiate, Reject: gate.Reject})
	if len(ds) > 0 {
		return nil, ds
	}
	for _, pkg := range program.Packages {
		if ds := gate.Check(pkg); len(ds) > 0 {
			return nil, distinct(ds)
		}
	}
	prog, ds := lower.LowerProgram(program)
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

// distinct returns ds without repetitions, which the gate reports once per
// instance of the generic code they are in (D-045).
func distinct(ds []diag.Diagnostic) []diag.Diagnostic {
	seen := map[diag.Diagnostic]bool{}
	var out []diag.Diagnostic
	for _, d := range ds {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}
