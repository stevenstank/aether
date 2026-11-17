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
// may only be rejected as invalid, for an overflowing constant. Since the
// D-006 experiment, every function of an accepted program is also promoted
// to SSA form, which must give the same interpreter results
// (checkPromotion). Since experiment RA-0 (D-026), the promoted program is
// also emitted with registers, which checks every allocation, and run
// natively when it finishes (checkRegistersGenerated). Since Phase 7
// increment 7.2, each seed's slice program goes through the same checks,
// since Phase 8 increment 8.1 its method program, since 8.2 its func value
// program, and since 8.3 its closure program.
func FuzzGenerated(f *testing.F) {
	f.Add(uint64(1))
	f.Fuzz(func(t *testing.T, seed uint64) {
		// The slice program of the seed (gen_slice_test.go), which is
		// never invalid.
		slices := []byte(generateSlices(seed))
		if ds := compileAndRun(t, slices); len(ds) > 0 {
			t.Fatalf("slice program rejected: %s\n%s", ds[0], slices)
		}
		checkRegistersGenerated(t, slices, checkPromotion(t, "gen.go", slices, 1_000_000, &promoteStats{}))
		// Likewise its method program (gen_method_test.go), since Phase 8
		// increment 8.1.
		methods := []byte(generateMethods(seed))
		if ds := compileAndRun(t, methods); len(ds) > 0 {
			t.Fatalf("method program rejected: %s\n%s", ds[0], methods)
		}
		checkRegistersGenerated(t, methods, checkPromotion(t, "gen.go", methods, 1_000_000, &promoteStats{}))
		// And its func value program (gen_funcvalue_test.go), since
		// increment 8.2.
		funcs := []byte(generateFuncValues(seed))
		if ds := compileAndRun(t, funcs); len(ds) > 0 {
			t.Fatalf("func value program rejected: %s\n%s", ds[0], funcs)
		}
		checkRegistersGenerated(t, funcs, checkPromotion(t, "gen.go", funcs, 1_000_000, &promoteStats{}))
		// And its closure program (gen_closure_test.go), since 8.3.
		closures := []byte(generateClosures(seed))
		if ds := compileAndRun(t, closures); len(ds) > 0 {
			t.Fatalf("closure program rejected: %s\n%s", ds[0], closures)
		}
		checkRegistersGenerated(t, closures, checkPromotion(t, "gen.go", closures, 1_000_000, &promoteStats{}))
		src := []byte(generate(seed))
		ds := compileAndRun(t, src)
		if len(ds) > 0 {
			if diag.ExitCode(ds) != diag.ExitInvalid {
				t.Fatalf("generated program rejected: %s\n%s", ds[0], src)
			}
			return
		}
		finished := checkPromotion(t, "gen.go", src, 1_000_000, &promoteStats{})
		checkRegistersGenerated(t, src, finished)
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
