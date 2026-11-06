package main

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
)

// Experiment RA-0 (D-026): SSA functions with values in registers. Emitting
// with amd64.EmitOptions{Registers: true} checks every allocation
// (checkAllocation), so every test here also runs the checker on every
// function it builds.

// emitRegisters compiles src, promotes it, and emits it with registers,
// twice: the assembly must be the same both times.
func emitRegisters(t *testing.T, name string, src []byte) []byte {
	t.Helper()
	return emitPromoted(t, name, src, amd64.EmitOptions{Registers: true})
}

// emitPromoted is emitRegisters with any options.
func emitPromoted(t *testing.T, name string, src []byte, opts amd64.EmitOptions) []byte {
	t.Helper()
	var asm [2][]byte
	for i := range asm {
		prog, ds := compile([]source{{name, src}})
		if len(ds) > 0 {
			t.Fatalf("%s: %v", name, ds)
		}
		if err := promoteProgram(prog, &promoteStats{}); err != nil {
			t.Fatalf("%s: promotion: %v", name, err)
		}
		var err error
		if asm[i], err = amd64.EmitWith(prog, opts); err != nil {
			t.Fatalf("%s: %v\nsource:\n%s", name, err, src)
		}
	}
	if !bytes.Equal(asm[0], asm[1]) {
		t.Fatalf("%s: assembly with registers is not deterministic", name)
	}
	return asm[0]
}

// runRegisters builds asm into dir and runs it; the result must be want.
func runRegisters(t *testing.T, name string, asm []byte, dir string, want result) {
	t.Helper()
	exe := filepath.Join(dir, strings.TrimSuffix(name, ".go")+".ra")
	if ds, err := assemble(asm, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
		t.Fatalf("%s: not built: %v %v", name, ds, err)
	}
	if got := runExe(t, exe); got != want {
		t.Errorf("%s: native with registers %+v, interpreter %+v", name, got, want)
	}
}

// TestNativeRASuite builds every program of the S2 suite promoted and with
// registers. Each executable must give the interpreter's stderr and exit
// status, as the slot-based and SSA executables of TestNativeSSASuite do.
// It logs the static figures against SSA without registers.
func TestNativeRASuite(t *testing.T) {
	if testing.Short() {
		t.Skip("builds about 240 executables")
	}
	requireTools(t)
	progs, _ := s2Suite(t)
	dir := t.TempDir()
	var ssa, ra codeStats
	larger := 0
	for _, p := range progs {
		want, ds, err := interpret(p.name, p.src)
		if len(ds) > 0 || err != nil {
			t.Fatalf("%s: not interpreted: %v %v", p.name, ds, err)
		}
		asm := emitRegisters(t, p.name, p.src)
		runRegisters(t, p.name, asm, dir, want)
		prog, _ := compile([]source{{p.name, p.src}})
		if err := promoteProgram(prog, &promoteStats{}); err != nil {
			t.Fatal(err)
		}
		plain, err := amd64.Emit(prog)
		if err != nil {
			t.Fatal(err)
		}
		s, sStack := measureAsm(t, plain)
		r, rStack := measureAsm(t, asm)
		ssa.mainStack += sStack
		ra.mainStack += rStack
		for _, name := range slices.Sorted(maps.Keys(s)) {
			ssa.add(s[name])
			ra.add(r[name])
			if r[name].instrs > s[name].instrs || r[name].frame > s[name].frame {
				larger++
			}
		}
	}
	t.Logf("%d programs built with registers and run; all match the interpreter", len(progs))
	t.Logf("machine instructions: SSA %d, RA-0 %d (%s)", ssa.instrs, ra.instrs, ratio(ra.instrs, ssa.instrs))
	t.Logf("frame bytes, summed over functions: SSA %d, RA-0 %d (%s); largest frame: SSA %d, RA-0 %d",
		ssa.frame, ra.frame, ratio(ra.frame, ssa.frame), ssa.maxFrame, ra.maxFrame)
	t.Logf("aether.mainstack, summed over programs: SSA %d, RA-0 %d (%s)", ssa.mainStack, ra.mainStack, ratio(ra.mainStack, ssa.mainStack))
	if larger > 0 {
		t.Errorf("%d functions have more instructions or a larger frame with registers", larger)
	}
}

// checkRegistersGenerated is FuzzGenerated's check of a valid generated
// program: emitting it as aether build does, with registers and selection,
// must succeed, which checks every allocation and selection, and be
// deterministic. If the program finishes in the interpreter within steps and
// as and ld are available, the native executable must also give the
// interpreter's result.
func checkRegistersGenerated(t *testing.T, src []byte, finished bool) {
	t.Helper()
	asm := emitPromoted(t, "gen.go", src, amd64.EmitOptions{Registers: true, Select: true})
	if !finished {
		return
	}
	for _, tool := range []string{"as", "ld"} {
		if _, err := exec.LookPath(tool); err != nil {
			return
		}
	}
	want, ds, err := interpret("gen.go", src)
	if len(ds) > 0 || err != nil {
		t.Fatalf("not interpreted: %v %v", ds, err)
	}
	runRegisters(t, fmt.Sprintf("gen%d.go", len(src)), asm, t.TempDir(), want)
}
