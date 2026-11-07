package main

import (
	"fmt"
	"io"
	"maps"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
)

// The register forms of arith.go (EmitOptions.TwoAddress), which aether build
// uses. Emitting checks every one with checkArith; these tests run them.

var twoAddrOps = []string{"+", "-", "*", "&", "|", "^", "&^"}

// twoAddrProgram is a program that runs every register form for type t on
// every pair of selectValues, in leaf functions whose operands die at the
// operation, outlive it, or are the same value, and in main, where the
// operands are in cells. It converts every value to every type.
func twoAddrProgram(t selectType) []byte {
	var b strings.Builder
	vals := selectValues(t)
	T := t.name
	b.WriteString("package main\n\nvar h uint64\n\nfunc m(v uint64) { h = h*1000003 + v }\n")
	fmt.Fprintf(&b, "\nfunc val(i int) %s {\n\tswitch i {\n", T)
	for i, v := range vals {
		fmt.Fprintf(&b, "\tcase %d:\n\t\treturn %s\n", i, v)
	}
	b.WriteString("\t}\n\treturn 0\n}\n")
	for k, op := range twoAddrOps {
		fmt.Fprintf(&b, "\nfunc a%d(x, y %s) %s { return x %s y }\n", k, T, T, op)
		fmt.Fprintf(&b, "\nfunc b%d(x, y %s) (%s, %s) { return x %s y, x }\n", k, T, T, T, op)
		fmt.Fprintf(&b, "\nfunc c%d(x, y %s) (%s, %s) { return x %s y, y }\n", k, T, T, T, op)
		fmt.Fprintf(&b, "\nfunc d%d(x, y %s) (%s, %s, %s) {\n\tr := x %s y\n\treturn r, x, y\n}\n", k, T, T, T, T, op)
		fmt.Fprintf(&b, "\nfunc e%d(x %s) %s { return x %s x }\n", k, T, T, op)
		fmt.Fprintf(&b, "\nfunc f%d(x, y, z %s) %s { return (x %s y) %s (z %s x) }\n", k, T, T, op, op, op)
	}
	fmt.Fprintf(&b, "\nfunc neg(x %s) (%s, %s) { return -x, x }\n\nfunc com(x %s) (%s, %s) { return ^x, x }\n", T, T, T, T, T, T)
	fmt.Fprintf(&b, "\nfunc neg1(x %s) %s { return -x }\n\nfunc com1(x %s) %s { return ^x }\n", T, T, T, T)
	for _, u := range selectTypes {
		fmt.Fprintf(&b, "\nfunc cv_%s(x %s) %s { return %s(x) }\n", u.name, T, u.name, u.name)
		fmt.Fprintf(&b, "\nfunc cw_%s(x %s) (%s, %s) { return %s(x), x }\n", u.name, T, u.name, T, u.name)
	}
	fmt.Fprintf(&b, "\nfunc main() {\n\tfor i := 0; i < %d; i++ {\n\t\tx := val(i)\n", len(vals))
	b.WriteString("\t\tp, q := neg(x)\n\t\tm(uint64(p))\n\t\tm(uint64(q))\n\t\tp, q = com(x)\n\t\tm(uint64(p))\n\t\tm(uint64(q))\n")
	b.WriteString("\t\tm(uint64(neg1(x)))\n\t\tm(uint64(com1(x)))\n\t\tm(uint64(-x))\n\t\tm(uint64(^x))\n")
	for _, u := range selectTypes {
		fmt.Fprintf(&b, "\t\tm(uint64(cv_%s(x)))\n\t\t{\n\t\t\tc, d := cw_%s(x)\n\t\t\tm(uint64(c))\n\t\t\tm(uint64(d))\n\t\t}\n\t\tm(uint64(%s(x)))\n", u.name, u.name, u.name)
	}
	fmt.Fprintf(&b, "\t\tfor j := 0; j < %d; j++ {\n\t\t\ty := val(j)\n", len(vals))
	for k, op := range twoAddrOps {
		fmt.Fprintf(&b, "\t\t\tm(uint64(a%d(x, y)))\n", k)
		fmt.Fprintf(&b, "\t\t\t{\n\t\t\t\tr, s := b%d(x, y)\n\t\t\t\tm(uint64(r))\n\t\t\t\tm(uint64(s))\n\t\t\t\tr, s = c%d(x, y)\n\t\t\t\tm(uint64(r))\n\t\t\t\tm(uint64(s))\n", k, k)
		fmt.Fprintf(&b, "\t\t\t\tu, v, w := d%d(x, y)\n\t\t\t\tm(uint64(u))\n\t\t\t\tm(uint64(v))\n\t\t\t\tm(uint64(w))\n\t\t\t}\n", k)
		fmt.Fprintf(&b, "\t\t\tm(uint64(e%d(y)))\n\t\t\tm(uint64(f%d(x, y, x %s y)))\n", k, k, op)
		// In main x and y are live across calls, so they are in cells.
		fmt.Fprintf(&b, "\t\t\tm(uint64(x %s y))\n\t\t\tm(uint64(y %s x))\n", op, op)
	}
	b.WriteString("\t\t}\n\t\tprintln(h)\n\t}\n}\n")
	return []byte(b.String())
}

// TestTwoAddressDifferential runs the programs of twoAddrProgram built by gc,
// by aether build, and with -O0, and in the interpreter: all four must agree.
func TestTwoAddressDifferential(t *testing.T) {
	requireTools(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	progs := map[string][]byte{}
	for _, ty := range selectTypes {
		progs["two_"+ty.name] = twoAddrProgram(ty)
	}
	want := gcRun(t, gobin, progs)
	dir := t.TempDir()
	// Every form of binaryInPlace, which the allocator chooses, appears.
	forms := map[string]*regexp.Regexp{
		"leaq":            regexp.MustCompile(`\tleaq\t\(%r\w+,%r\w+\), %r\w+\n`),
		"sub into y":      regexp.MustCompile(`\tnegq\t(%r\w+)\n\taddq\t%r\w+, (%r\w+)\n`),
		"and-not into y":  regexp.MustCompile(`\tnotq\t(%r[^c]\w*)\n\tandq\t%r\w+, %r\w+\n`),
		"and-not into x":  regexp.MustCompile(`\tnotq\t%rcx\n\tandq\t%rcx, %r\w+\n`),
		"extend directly": regexp.MustCompile(`\tmovs[bwl]q\t%\w+, %r\w+\n`),
	}
	seen := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(progs)) {
		got, ds, err := interpret(name+".go", progs[name])
		if len(ds) > 0 || err != nil {
			t.Fatalf("%s: not interpreted: %v %v", name, ds, err)
		}
		if got != want[name] {
			t.Errorf("%s: interpreter differs from gc:\n%s", name, diffResults(got, want[name]))
		}
		prog, ds := compile([]source{{name + ".go", progs[name]}})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		asm, err := emitProgram(prog)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for f, re := range forms {
			seen[f] = seen[f] || re.Match(asm)
		}
		for _, o0 := range []bool{false, true} {
			exe := filepath.Join(dir, fmt.Sprintf("%s.%v", name, o0))
			ds, err := build([]source{{name + ".go", progs[name]}}, buildOptions{out: exe, o0: o0}, io.Discard)
			if len(ds) > 0 || err != nil {
				t.Fatalf("%s: -O0 %v: not built: %v %v", name, o0, ds, err)
			}
			if got := runExe(t, exe); got != want[name] {
				t.Errorf("%s: -O0 %v: native differs from gc:\n%s", name, o0, diffResults(got, want[name]))
			}
		}
	}
	for _, f := range slices.Sorted(maps.Keys(forms)) {
		if !seen[f] {
			t.Errorf("no program uses the form %q", f)
		}
	}
}

// TestTwoAddressNoLarger checks that the register forms never make a function
// longer or its frame larger, on the S2 suite and the workloads, and that the
// default build uses them.
func TestTwoAddressNoLarger(t *testing.T) {
	progs, _ := s2Suite(t)
	for _, w := range benchWorkloads(t) {
		progs = append(progs, s2Program{w.name + ".go", w.src})
	}
	without := amd64.EmitOptions{Registers: true, Select: true, ABIR1: true}
	with := without
	with.TwoAddress = true
	var before, after codeStats
	for _, p := range progs {
		two, tStack := measureAsm(t, emitPromoted(t, p.name, p.src, with))
		acc, aStack := measureAsm(t, emitPromoted(t, p.name, p.src, without))
		if tStack > aStack {
			t.Errorf("%s: aether.mainstack %d with the register forms, %d without", p.name, tStack, aStack)
		}
		for _, fn := range slices.Sorted(maps.Keys(acc)) {
			before.add(acc[fn])
			after.add(two[fn])
			if two[fn].instrs > acc[fn].instrs || two[fn].frame > acc[fn].frame {
				t.Errorf("%s: %s: %d instructions and frame %d with the register forms, %d and %d without",
					p.name, fn, two[fn].instrs, two[fn].frame, acc[fn].instrs, acc[fn].frame)
			}
		}
	}
	if after.instrs >= before.instrs {
		t.Errorf("the register forms remove no instruction: %d with, %d without", after.instrs, before.instrs)
	}
	t.Logf("machine instructions: without %d, with the register forms %d (%s)", before.instrs, after.instrs, ratio(after.instrs, before.instrs))
}
