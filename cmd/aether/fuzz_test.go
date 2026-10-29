package main

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

// FuzzCheck checks that no input, valid or not, makes check panic or report
// an internal error (docs/ROADMAP.md, Phase 1). Since Phase 2, check lowers
// and verifies every accepted program, and the IR is also interpreted, which
// must succeed. The corpus files seed it.
func FuzzCheck(f *testing.F) {
	for _, path := range corpus(f) {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		compileAndRun(t, src)
	})
}

// FuzzGenerated runs generated S1 programs through lowering, the verifier,
// and the interpreter. Unlike FuzzCheck, whose inputs are mostly rejected
// before lowering, nearly every input here reaches it. A generated program
// may only be rejected as invalid, for an overflowing constant.
func FuzzGenerated(f *testing.F) {
	f.Add(uint64(1))
	f.Fuzz(func(t *testing.T, seed uint64) {
		src := []byte(generate(seed))
		if ds := compileAndRun(t, src); len(ds) > 0 && diag.ExitCode(ds) != diag.ExitInvalid {
			t.Fatalf("generated program rejected: %s\n%s", ds[0], src)
		}
	})
}

// compileAndRun compiles src, failing on a panic or internal error, then
// interprets the IR if there is any, failing if that does not succeed. A
// valid program may loop forever, so running out of the step budget is not
// a failure.
func compileAndRun(t *testing.T, src []byte) []diag.Diagnostic {
	t.Helper()
	var prog *ir.Program
	ds := guard(func() (ds []diag.Diagnostic) {
		prog, ds = compile([]source{{name: "fuzz.go", data: src}})
		return ds
	})
	for _, d := range ds {
		if d.Class == diag.Internal {
			t.Fatalf("internal error: %s\nsource:\n%s", d, src)
		}
	}
	if prog != nil {
		if _, err := interp.RunSteps(prog, io.Discard, 1_000_000); err != nil && !errors.Is(err, interp.ErrSteps) {
			t.Fatalf("interpreter: %v\nsource:\n%s", err, src)
		}
	}
	return ds
}
