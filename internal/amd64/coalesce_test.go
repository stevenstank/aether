package amd64

import (
	"bytes"
	"maps"
	"regexp"
	"slices"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

// The options of aether build, without and with coalescing.
var (
	plainOptions    = EmitOptions{Registers: true, Select: true, ABIR1: true, TwoAddress: true}
	coalesceOptions = EmitOptions{Registers: true, Select: true, ABIR1: true, TwoAddress: true, Coalesce: true}
)

var reMoveRR = regexp.MustCompile(`(?m)^\tmovq\t%\w+, %\w+$`)

// fMoves emits the named test program and counts the register moves in f's
// code.
func fMoves(t *testing.T, name string, opts EmitOptions) int {
	t.Helper()
	p, _ := raBuild(t, name)
	asm, err := EmitWith(p, opts)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	body := asm[bytes.Index(asm, []byte("main.f:")):]
	body = body[:bytes.Index(body, []byte("\t.size"))]
	return len(reMoveRR.FindAll(body, -1))
}

// raCoalesce builds the named function, allocates it with coalescing, and
// checks the allocation.
func raCoalesce(t *testing.T, name string) (*ir.Func, map[*ir.Instr]int) {
	t.Helper()
	_, f := raBuild(t, name)
	regs, err := allocateExcept(f, nil, true)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := checkAllocation(f, regs); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, describe(f, regs))
	}
	return f, regs
}

// regOf returns the register of value id, or "cell".
func regOf(t *testing.T, f *ir.Func, regs map[*ir.Instr]int, id int) string {
	t.Helper()
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if in.ID == id {
				if r, ok := regs[in]; ok {
					return allocRegs[r]
				}
				return "cell"
			}
		}
	}
	t.Fatalf("no v%d", id)
	return ""
}

// TestCoalesce checks where coalescing puts values of the test functions:
// each allocation passes checkAllocation, and every function but call, whose
// values are live across its call and keep their cells, has fewer register
// moves with it, and none has more.
func TestCoalesce(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(raFuncs)) {
		raCoalesce(t, name)
		without, with := fMoves(t, name, plainOptions), fMoves(t, name, coalesceOptions)
		if with > without || with == without && name != "call" {
			t.Errorf("%s: %d register moves with coalescing, %d without", name, with, without)
		}
	}
	same := func(name string, ids ...int) {
		t.Helper()
		f, regs := raCoalesce(t, name)
		for _, id := range ids[1:] {
			if a, b := regOf(t, f, regs, ids[0]), regOf(t, f, regs, id); a != b || a == "cell" {
				t.Errorf("%s: v%d in %s and v%d in %s, want one register:\n%s", name, ids[0], a, id, b, describe(f, regs))
			}
		}
	}
	// The loop's φs share their back edges' values' registers.
	same("loop", 3, 6)
	same("loop", 4, 7)
	// Every x of the CRC-like loop, in either arm and after them: no edge
	// of the loop moves it.
	same("shift", 2, 11, 13, 15, 16)
	// The diamond's arms compute in the φ's register.
	same("diamond", 3, 5, 6)
	// The six parameters stay in their argument registers, and the sum
	// accumulates in the first one's.
	f, regs := raCoalesce(t, "straight")
	for i, r := range argRegs {
		if got := regOf(t, f, regs, i); got != r {
			t.Errorf("straight: param %d in %s, want %s", i, got, r)
		}
	}
	same("straight", 0, 6, 7, 8, 9, 10)
	// p0+1 is computed in the register it is passed in.
	f, regs = raCoalesce(t, "args")
	if got := regOf(t, f, regs, 4); got != argRegs[2] {
		t.Errorf("args: v4 in %s, want %s:\n%s", got, argRegs[2], describe(f, regs))
	}
}

// TestCoalesceRefuses checks preferences that a free register cannot meet: a
// preference never puts two interfering values in one register.
func TestCoalesceRefuses(t *testing.T) {
	// j (v6) is the back-edge value of i (v4), but i is live after j's
	// definition.
	f, regs := raCoalesce(t, "busy")
	if a, b := regOf(t, f, regs, 4), regOf(t, f, regs, 6); a == b {
		t.Errorf("busy: i and j share %s:\n%s", a, describe(f, regs))
	}
	// a (v3) and b (v4) of the swap take each other's values: each prefers
	// the other's register, and both are live into the loop.
	f, regs = raCoalesce(t, "swap")
	if a, b := regOf(t, f, regs, 3), regOf(t, f, regs, 4); a == b {
		t.Errorf("swap: a and b share %s:\n%s", a, describe(f, regs))
	}
	// args: -p0 (v2) is passed in %rsi, which p1 (v1), passed first,
	// holds until the call.
	f, regs = raCoalesce(t, "args")
	if got := regOf(t, f, regs, 2); got == argRegs[1] {
		t.Errorf("args: v2 in %s, which v1 holds:\n%s", got, describe(f, regs))
	}
	// call: values live across the call keep their cells.
	f, regs = raCoalesce(t, "call")
	for _, id := range []int{0, 1, 2} {
		if got := regOf(t, f, regs, id); got != "cell" {
			t.Errorf("call: v%d in %s, want its cell", id, got)
		}
	}
}

// TestCoalesceDeterministic checks that coalescing depends only on the
// function.
func TestCoalesceDeterministic(t *testing.T) {
	for name := range raFuncs {
		f, want := raCoalesce(t, name)
		for range 5 {
			got, err := allocateExcept(f, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			if describe(f, got) != describe(f, want) {
				t.Fatalf("%s: allocation differs between runs:\n%s\n%s", name, describe(f, got), describe(f, want))
			}
		}
	}
}

// TestNativeCoalesce runs every test program as aether build emits it, with
// and without coalescing, natively: both must print what the interpreter
// does.
func TestNativeCoalesce(t *testing.T) {
	for name := range raFuncs {
		p, _ := raBuild(t, name)
		var want bytes.Buffer
		if code, err := interp.Run(p, &want); err != nil || code != 0 {
			t.Fatalf("%s: interpreter: exit %d, %v", name, code, err)
		}
		for _, opts := range []EmitOptions{plainOptions, coalesceOptions} {
			asm, err := EmitWith(p, opts)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if out, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 0 || out != want.String() {
				t.Errorf("%s: coalesce %v: native exit %d, output differs from the interpreter's:\n%s",
					name, opts.Coalesce, code, diffLine(out, want.String()))
			}
		}
	}
}
