package main

import (
	"maps"
	"slices"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
)

// TestFoldNoLarger checks that constant folding (opt.Fold, in aether build's
// pipeline) never makes a function longer or its frame larger, on the S2
// suite and the workloads, and that it removes code. TestPromoteSuite checks
// the folded IR against the interpreter; the native suites build through the
// same pipeline.
func TestFoldNoLarger(t *testing.T) {
	progs, _ := s2Suite(t)
	for _, w := range benchWorkloads(t) {
		progs = append(progs, s2Program{w.name + ".go", w.src})
	}
	opts := amd64.EmitOptions{Registers: true, Select: true, ABIR1: true, TwoAddress: true}
	emit := func(p s2Program, fold bool) map[string]funcCode {
		prog, ds := compile([]source{{p.name, p.src}})
		if len(ds) > 0 {
			t.Fatalf("%s: %v", p.name, ds)
		}
		if err := optimize(prog, fold); err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		asm, err := amd64.EmitWith(prog, opts)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		code, _ := measureAsm(t, asm)
		return code
	}
	var before, after codeStats
	for _, p := range progs {
		plain, folded := emit(p, false), emit(p, true)
		for _, fn := range slices.Sorted(maps.Keys(plain)) {
			before.add(plain[fn])
			after.add(folded[fn])
			if folded[fn].instrs > plain[fn].instrs || folded[fn].frame > plain[fn].frame {
				t.Errorf("%s: %s: %d instructions and frame %d folded, %d and %d not",
					p.name, fn, folded[fn].instrs, folded[fn].frame, plain[fn].instrs, plain[fn].frame)
			}
		}
	}
	if after.instrs >= before.instrs {
		t.Errorf("folding removes no instruction: %d folded, %d not", after.instrs, before.instrs)
	}
	t.Logf("machine instructions: %d not folded, %d folded (%s)", before.instrs, after.instrs, ratio(after.instrs, before.instrs))
}
