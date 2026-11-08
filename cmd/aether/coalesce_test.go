package main

import (
	"maps"
	"regexp"
	"slices"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
)

// reMoveRR matches a move between two registers.
var reMoveRR = regexp.MustCompile(`(?m)^\tmovq\t%\w+, %\w+$`)

// TestCoalesceNoLarger checks, on the S2 suite and the workloads built by the
// default build's pipeline, that coalescing (EmitOptions.Coalesce) never makes a
// function longer or its frame larger, and that it removes register moves.
func TestCoalesceNoLarger(t *testing.T) {
	without := r1Options
	without.Coalesce = false
	emit := func(p s2Program, opts amd64.EmitOptions) []byte {
		prog, ds := compile([]source{{p.name, p.src}})
		if len(ds) > 0 {
			t.Fatalf("%s: %v", p.name, ds)
		}
		asm, err := emitProgramWith(prog, opts)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		return asm
	}
	var before, after codeStats
	var movesBefore, movesAfter int
	for _, p := range passPrograms(t) {
		plain, coalesced := emit(p, without), emit(p, r1Options)
		movesBefore += len(reMoveRR.FindAll(plain, -1))
		movesAfter += len(reMoveRR.FindAll(coalesced, -1))
		acc, aStack := measureAsm(t, plain)
		co, cStack := measureAsm(t, coalesced)
		if cStack > aStack {
			t.Errorf("%s: aether.mainstack %d with coalescing, %d without", p.name, cStack, aStack)
		}
		for _, fn := range slices.Sorted(maps.Keys(acc)) {
			before.add(acc[fn])
			after.add(co[fn])
			if co[fn].instrs > acc[fn].instrs || co[fn].frame > acc[fn].frame {
				t.Errorf("%s: %s: %d instructions and frame %d with coalescing, %d and %d without",
					p.name, fn, co[fn].instrs, co[fn].frame, acc[fn].instrs, acc[fn].frame)
			}
		}
	}
	if movesAfter >= movesBefore || after.instrs >= before.instrs {
		t.Errorf("coalescing removes nothing: %d register moves and %d instructions with it, %d and %d without",
			movesAfter, after.instrs, movesBefore, before.instrs)
	}
	t.Logf("register moves: without %d, with coalescing %d (%s)", movesBefore, movesAfter, ratio(movesAfter, movesBefore))
	t.Logf("machine instructions: without %d, with coalescing %d (%s)", before.instrs, after.instrs, ratio(after.instrs, before.instrs))
}
