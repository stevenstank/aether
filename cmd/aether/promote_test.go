package main

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
	"github.com/stevenstank/aether/internal/opt"
)

// promoteStats counts what promotion changed in a program's functions.
type promoteStats struct {
	funcs, slots, loads, stores, phis, before, after int
	folded                                           int // IR instructions after folding
}

// foldProgram folds every SSA function of prog, adding its size to st, and
// verifies the result. A second Fold must change nothing.
func foldProgram(prog *ir.Program, st *promoteStats) error {
	for _, f := range prog.Funcs {
		if !f.SSA {
			continue
		}
		if _, err := opt.Fold(f); err != nil {
			return err
		}
		if again, err := opt.Fold(f); again || err != nil {
			return fmt.Errorf("func %s: a second Fold changed it (%v)", f.Name, err)
		}
		for _, b := range f.Blocks {
			st.folded += len(b.Instrs)
		}
	}
	return errors.Join(ir.Verify(prog)...)
}

// promoteProgram converts every function of prog to SSA form (D-006
// experiment, D-025), adding to st, and verifies the result.
func promoteProgram(prog *ir.Program, st *promoteStats) error {
	count := func(f *ir.Func, op ir.Op) (n int) {
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				if in.Op == op {
					n++
				}
			}
		}
		return n
	}
	size := func(f *ir.Func) (n int) {
		for _, b := range f.Blocks {
			n += len(b.Instrs)
		}
		return n
	}
	for _, f := range prog.Funcs {
		st.funcs++
		st.slots += len(f.Slots)
		st.loads += count(f, ir.Load)
		st.stores += count(f, ir.Store)
		st.before += size(f)
		if err := ir.Promote(f); err != nil {
			return err
		}
		st.phis += count(f, ir.Phi)
		st.after += size(f)
	}
	return errors.Join(ir.Verify(prog)...)
}

// checkPromotion lowers src twice, promotes one copy, and requires the
// interpreter to give identical results for both, with the given step
// budget. If the ordinary program exhausts the budget, nothing is
// compared: the promoted one executes fewer steps, because it has no loads
// or stores and phis do not count. It reports whether the results were
// compared.
func checkPromotion(t *testing.T, name string, src []byte, steps int64, st *promoteStats) bool {
	t.Helper()
	prog, ds := compile([]source{{name, src}})
	if len(ds) > 0 {
		t.Fatalf("%s: %v", name, ds)
	}
	promoted, _ := compile([]source{{name, src}})
	if err := promoteProgram(promoted, st); err != nil {
		t.Fatalf("%s: promotion: %v\n%s", name, err, promoted)
	}
	again, _ := compile([]source{{name, src}})
	if err := promoteProgram(again, &promoteStats{}); err != nil || again.String() != promoted.String() {
		t.Fatalf("%s: promotion is not deterministic: %v", name, err)
	}
	var want, got bytes.Buffer
	wantCode, wantErr := interp.RunSteps(prog, &want, steps)
	if errors.Is(wantErr, interp.ErrSteps) {
		return false
	}
	gotCode, gotErr := interp.RunSteps(promoted, &got, steps)
	if wantErr != nil || gotErr != nil || gotCode != wantCode || got.String() != want.String() {
		t.Fatalf("%s: interpreter results differ after promotion:\nbefore: exit %d, %v, stderr:\n%s\nafter: exit %d, %v, stderr:\n%s\nSSA:\n%s",
			name, wantCode, wantErr, want.String(), gotCode, gotErr, got.String(), promoted)
	}
	// Folding, after the same promotion, must verify, be deterministic, and
	// keep the results. It removes code, so it runs in no more steps.
	if err := foldProgram(promoted, st); err != nil {
		t.Fatalf("%s: folding: %v\n%s", name, err, promoted)
	}
	if err := foldProgram(again, &promoteStats{}); err != nil || again.String() != promoted.String() {
		t.Fatalf("%s: folding is not deterministic: %v", name, err)
	}
	var folded bytes.Buffer
	foldCode, foldErr := interp.RunSteps(promoted, &folded, steps)
	if foldErr != nil || foldCode != wantCode || folded.String() != want.String() {
		t.Fatalf("%s: interpreter results differ after folding:\nbefore: exit %d, stderr:\n%s\nafter: exit %d, %v, stderr:\n%s\nfolded:\n%s",
			name, wantCode, want.String(), foldCode, foldErr, folded.String(), promoted)
	}
	return true
}

// s2Program is a program of the S2 suite.
type s2Program struct {
	name string
	src  []byte
}

// s2Suite returns the programs of the S2 suite (TESTING.md), in a fixed
// order: the corpus programs expected to run, the first generatedPrograms
// valid generated programs, and the stack-overflow programs. It also
// returns how many came from the corpus.
func s2Suite(t *testing.T) (progs []s2Program, corpusRun int) {
	t.Helper()
	for _, path := range corpus(t) {
		if readExpectation(t, path).class != "run" {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs = append(progs, s2Program{filepath.Base(path), src})
	}
	corpusRun = len(progs)
	for seed := uint64(1); len(progs) < corpusRun+generatedPrograms; seed++ {
		if seed > 3*generatedPrograms {
			t.Fatal("too many generated programs rejected")
		}
		src := []byte(generate(seed))
		if _, ds := compile([]source{{"gen.go", src}}); len(ds) == 0 {
			progs = append(progs, s2Program{fmt.Sprintf("gen%04d.go", seed), src})
		}
	}
	names := slices.Sorted(maps.Keys(overflowPrograms))
	for _, name := range names {
		progs = append(progs, s2Program{name + ".go", []byte("package main\n\n" + overflowPrograms[name] + "\n")})
	}
	return progs, corpusRun
}

// TestPromoteSuite promotes every program of the S2 suite. The promoted IR
// must verify, promotion must be deterministic, and the interpreter must
// give the same output and exit status before and after it.
func TestPromoteSuite(t *testing.T) {
	progs, corpusRun := s2Suite(t)
	var st promoteStats
	for _, p := range progs {
		if !checkPromotion(t, p.name, p.src, interpSteps, &st) {
			t.Errorf("%s: exhausted the step budget", p.name)
		}
	}
	t.Logf("%d programs (%d corpus, %d generated, %d overflow); %d functions, %d slots; "+
		"%d loads and %d stores removed, %d phis added; IR instructions %d before, %d after, %d folded",
		len(progs), corpusRun, generatedPrograms, len(overflowPrograms), st.funcs, st.slots,
		st.loads, st.stores, st.phis, st.before, st.after, st.folded)
}
