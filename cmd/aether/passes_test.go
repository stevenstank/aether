package main

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
	"github.com/stevenstank/aether/internal/ir"
	"github.com/stevenstank/aether/internal/opt"
)

// passPrograms are the S2 suite and the workloads.
func passPrograms(t *testing.T) []s2Program {
	progs, _ := s2Suite(t)
	for _, w := range benchWorkloads(t) {
		progs = append(progs, s2Program{w.name + ".go", w.src})
	}
	return progs
}

// TestPassesNoLarger checks that each pass of buildPasses, added to those
// before it, never makes a function longer or its frame larger, on the S2
// suite and the workloads. TestPromoteSuite checks the passes' IR against the
// interpreter; the native suites build through the same pipeline.
func TestPassesNoLarger(t *testing.T) {
	progs := passPrograms(t)
	opts := amd64.EmitOptions{Registers: true, Select: true, ABIR1: true, TwoAddress: true}
	emit := func(p s2Program, passes []pass) map[string]funcCode {
		prog, ds := compile([]source{{p.name, p.src}})
		if len(ds) > 0 {
			t.Fatalf("%s: %v", p.name, ds)
		}
		if err := optimize(prog, passes...); err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		asm, err := amd64.EmitWith(prog, opts)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		code, _ := measureAsm(t, asm)
		return code
	}
	for n := 1; n <= len(buildPasses); n++ {
		var before, after codeStats
		for _, p := range progs {
			plain, passed := emit(p, buildPasses[:n-1]), emit(p, buildPasses[:n])
			for _, fn := range slices.Sorted(maps.Keys(plain)) {
				before.add(plain[fn])
				after.add(passed[fn])
				if passed[fn].instrs > plain[fn].instrs || passed[fn].frame > plain[fn].frame {
					t.Errorf("pass %d: %s: %s: %d instructions and frame %d with it, %d and %d without",
						n, p.name, fn, passed[fn].instrs, passed[fn].frame, plain[fn].instrs, plain[fn].frame)
				}
			}
		}
		if after.instrs >= before.instrs {
			t.Errorf("pass %d removes no instruction: %d with it, %d without", n, after.instrs, before.instrs)
		}
		t.Logf("pass %d: machine instructions %d without it, %d with it (%s)", n, before.instrs, after.instrs, ratio(after.instrs, before.instrs))
	}
}

// TestDCEKeepsEffects checks, on the S2 suite and the workloads after
// promotion and folding, that DCE keeps every instruction with an effect in
// its block and order, and leaves nothing it could remove.
func TestDCEKeepsEffects(t *testing.T) {
	effects := func(f *ir.Func) []string {
		var out []string
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				switch in.Op {
				case ir.Call, ir.Println, ir.Print, ir.GStore, ir.Param, ir.Ret, ir.Jump, ir.Branch, ir.Panic:
					out = append(out, fmt.Sprintf("b%d %s", b.ID, in))
				}
			}
		}
		return out
	}
	removed := 0
	for _, p := range passPrograms(t) {
		prog, ds := compile([]source{{p.name, p.src}})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		if err := optimize(prog, opt.Fold); err != nil {
			t.Fatal(err)
		}
		for _, f := range prog.Funcs {
			if !f.SSA {
				continue
			}
			want, size := effects(f), 0
			for _, b := range f.Blocks {
				size -= len(b.Instrs)
			}
			if _, err := opt.DCE(f); err != nil {
				t.Fatal(err)
			}
			if got := effects(f); !slices.Equal(got, want) {
				t.Errorf("%s: func %s: effects changed:\n%v\nwant:\n%v", p.name, f.Name, got, want)
			}
			uses := map[*ir.Instr]int{}
			for _, b := range f.Blocks {
				size += len(b.Instrs)
				for _, in := range b.Instrs {
					for _, a := range in.Args {
						if a != in {
							uses[a]++
						}
					}
				}
			}
			for _, b := range f.Blocks {
				for _, in := range b.Instrs {
					if in.Type != ir.NoType && in.Op != ir.Call && in.Op != ir.Param && uses[in] == 0 {
						t.Errorf("%s: func %s: %s is left unused", p.name, f.Name, in)
					}
				}
			}
			removed -= size
		}
	}
	t.Logf("DCE removed %d IR instructions", removed)
}
