package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

// promoteStats counts what promotion changed in a program's functions.
type promoteStats struct {
	funcs, slots, loads, stores, phis, before, after int
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
	return true
}

// TestPromoteSuite promotes every program of the S2 suite (TESTING.md): the
// corpus programs expected to run, the 200 generated programs of the
// differential test, and the stack-overflow programs. The promoted IR must
// verify, promotion must be deterministic, and the interpreter must give the
// same output and exit status before and after it.
func TestPromoteSuite(t *testing.T) {
	type prog struct {
		name string
		src  []byte
	}
	var progs []prog
	for _, path := range corpus(t) {
		if readExpectation(t, path).class != "run" {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs = append(progs, prog{filepath.Base(path), src})
	}
	corpusRun := len(progs)
	for seed := uint64(1); len(progs) < corpusRun+generatedPrograms; seed++ {
		if seed > 3*generatedPrograms {
			t.Fatal("too many generated programs rejected")
		}
		src := []byte(generate(seed))
		if _, ds := compile([]source{{"gen.go", src}}); len(ds) == 0 {
			progs = append(progs, prog{fmt.Sprintf("gen%04d.go", seed), src})
		}
	}
	for name, body := range overflowPrograms {
		progs = append(progs, prog{name + ".go", []byte("package main\n\n" + body + "\n")})
	}
	var st promoteStats
	for _, p := range progs {
		if !checkPromotion(t, p.name, p.src, interpSteps, &st) {
			t.Errorf("%s: exhausted the step budget", p.name)
		}
	}
	t.Logf("%d programs (%d corpus, %d generated, %d overflow); %d functions, %d slots; "+
		"%d loads and %d stores removed, %d phis added; IR instructions %d before, %d after",
		len(progs), corpusRun, generatedPrograms, len(overflowPrograms), st.funcs, st.slots,
		st.loads, st.stores, st.phis, st.before, st.after)
}
