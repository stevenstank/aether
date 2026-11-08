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
	"github.com/stevenstank/aether/internal/diag"
)

// ABI-R1 (D-030), the default build's convention since D-031. These tests emit
// through the default build's pipeline, and compare it with ABI-S0 emitted with
// the same pipeline and the option off. Emitting checks every
// entry, call, and return (checkR1).

var (
	r1Options = amd64.EmitOptions{Registers: true, Select: true, ABIR1: true, TwoAddress: true, Coalesce: true}
	s0Options = amd64.EmitOptions{Registers: true, Select: true, TwoAddress: true, Coalesce: true}
)

// emitR1 returns src's assembly under ABI-R1, emitted twice: it must not differ.
func emitR1(t *testing.T, name string, src []byte) []byte {
	t.Helper()
	var asm [2][]byte
	for i := range asm {
		prog, ds := compile([]source{{name, src}})
		if len(ds) > 0 {
			t.Fatalf("%s: %v", name, ds)
		}
		var err error
		if asm[i], err = emitProgramWith(prog, r1Options); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if !bytes.Equal(asm[0], asm[1]) {
		t.Fatalf("%s: assembly under ABI-R1 is not deterministic", name)
	}
	return asm[0]
}

// runR1 builds asm in dir and runs it.
func runR1(t *testing.T, name string, asm []byte, dir string) result {
	t.Helper()
	exe := filepath.Join(dir, strings.TrimSuffix(name, ".go")+".r1")
	if ds, err := assemble(asm, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
		t.Fatalf("%s: not built: %v %v", name, ds, err)
	}
	return runExe(t, exe)
}

// r1Permutations has a function for every order of six arguments. With x, the
// first parameter, passed on the stack by the call, the allocator leaves the
// other six parameters in exactly the argument registers, so the call's moves
// are that permutation of the registers, and all but the identity need cycles.
func r1Permutations() []byte {
	var b strings.Builder
	b.WriteString("package main\n\nvar h uint64\n\nfunc sink(a, b, c, d, e, f, x int) int {\n\treturn a + b*3 + c*5 + d*7 + e*11 + f*13 + x*17\n}\n")
	names := []string{"a", "b", "c", "d", "e", "f"}
	n := 0
	var calls strings.Builder
	var walk func(k int)
	walk = func(k int) {
		if k == len(names) {
			fmt.Fprintf(&b, "\nfunc p%d(x, a, b, c, d, e, f int) int { return sink(%s, x) }\n", n, strings.Join(names, ", "))
			fmt.Fprintf(&calls, "\th = h*31 + uint64(p%d(7, 101, 203, 307, 401, 503, 601))\n", n)
			n++
			return
		}
		for i := k; i < len(names); i++ {
			names[k], names[i] = names[i], names[k]
			walk(k + 1)
			names[k], names[i] = names[i], names[k]
		}
	}
	walk(0)
	b.WriteString("\nfunc main() {\n" + calls.String() + "\tprintln(h)\n}\n")
	return []byte(b.String())
}

// r1Programs test the convention itself.
var r1Programs = map[string]string{
	// 0 to 8 arguments and 0 to 3 results, with arguments as immediates, large
	// constants, registers, and cells: p stays in a cell, since it is live
	// across a call.
	"r1_arity": `package main

var g int

func a0() int { g++; return g }
func a1(a int) int { return a*3 + 1 }
func a2(a, b int) int { return a - b*2 }
func a3(a, b, c int) int { return a - b + c*5 }
func a4(a, b, c, d int) int { return a*2 - b*3 + c*5 - d*7 }
func a5(a, b, c, d, e int) int { return a - b*3 + c*5 - d*7 + e*11 }
func a6(a, b, c, d, e, f int) int { return a*2 - b*3 + c*5 - d*7 + e*11 - f*13 }
func a7(a, b, c, d, e, f, h int) int { return a - b*3 + c*5 - d*7 + e*11 - f*13 + h*17 }
func a8(a, b, c, d, e, f, h, i int) int { return a*2 - b*3 + c*5 - d*7 + e*11 - f*13 + h*17 - i*19 }
func v0() { g += 100 }
func v2(a, b int) { println(a, b) }
func r2(x int) (int, int) { return x + 1, x * 2 }
func r3(a, b int) (int, int, int) { return b, a, a - b }
func r38(a, b, c, d, e, f, h, i int) (int, int, int) { return i, a + h, b - f + c + d + e }
func wide(a int8, b uint16, c int32, d uint8, e bool, f uint64, h int16, i uint32) (int8, bool) {
	println(a, b, c, d, e, f, h, i)
	return a + int8(d), !e
}
func narrow(a uint8) uint8 { return a + 200 }
func unused(a, b, c int) int { return b }

func mix(x, y int) int {
	p := a0()
	q := a1(x)
	s := a6(x, y, p, q, 5, 1<<40)
	s += a7(y, p, x, q, -1, 9223372036854775807, s)
	s ^= a8(p, x, s, y, q, -9223372036854775808, 1<<33, x)
	s -= a2(y, x)
	return s + p + q
}

func main() {
	println(a0(), a1(3), a2(5, 7), a3(1, 2, 3), a4(1, 2, 3, 4), a5(5, 4, 3, 2, 1))
	println(a6(1, 2, 3, 4, 5, 6), a7(1, 2, 3, 4, 5, 6, 7), a8(1, 2, 3, 4, 5, 6, 7, 8))
	v0()
	v2(g, 9223372036854775807)
	x, y := r2(g)
	u, v, w := r3(x, y)
	k, l, m := r38(x, y, u, v, w, 6, 7, 8)
	println(x, y, u, v, w, k, l, m)
	println(mix(x, y), mix(-1, 1<<50))
	println(wide(-128, 65535, -2147483648, 255, true, 18446744073709551615, -32768, 4294967295))
	println(narrow(100), unused(1, 2, 3))
}
`,
	// Calls in arguments, recursion, and mutual recursion with arguments that
	// rotate through the registers.
	"r1_nested": `package main

func h(a int) int { return a + 1 }
func g(a, b int) int { return a*10 + b }
func two(a int) (int, int) { return h(a), g(a, a) }

func even(n, a, b, c int) int {
	if n == 0 {
		return a
	}
	return odd(n-1, b, c, a+1)
}

func odd(n, a, b, c int) int {
	if n == 0 {
		return b - c
	}
	return even(n-1, c, a, b*2%1000003)
}

func eight(n, a, b, c, d, e, f, x int) int {
	if n == 0 {
		return a + b*2 + c*3 + d*4 + e*5 + f*6 + x*7
	}
	return eight(n-1, x, a, b, c, d, e, f+g(a, b)) - n
}

func pair(n int) (int, int) {
	if n == 0 {
		return 0, 1
	}
	a, b := pair(n - 1)
	return b, a + b
}

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func ack(m, n int) int {
	if m == 0 {
		return n + 1
	}
	if n == 0 {
		return ack(m-1, 1)
	}
	return ack(m-1, ack(m, n-1))
}

func main() {
	println(g(h(1), g(h(2), h(g(3, 4)))))
	x, y := two(g(h(5), 6))
	println(x, y, even(101, 1, 2, 3), odd(100, 4, 5, 6))
	println(eight(50, 1, 2, 3, 4, 5, 6, 7))
	a, b := pair(40)
	println(a, b, fib(20), ack(2, 3))
}
`,
	// Panics in arguments, where gc's order is the same as D-024's: in a call,
	// or after every call of the statement (V-4).
	"r1_panic_arg": `package main

var zero int

func t(x int) int {
	println(x)
	return x
}
func f(a, b, c int) int { return a + b + c }

func main() {
	println(f(t(1), t(2), t(3)))
	println(f(t(1), t(2), t(3)/zero))
}
`,
	"r1_panic_shift": `package main

var neg = -1

func t(x int) int {
	println(x)
	return x
}
func s(x int) int { return 1 << x }
func f(a, b, c, d, e, g, h int) int { return a + b + c + d + e + g + h }

func main() {
	println(f(t(1), t(2), t(3), t(4), t(5), t(6), t(7)))
	println(f(t(1), t(2), t(3), t(4), t(5), s(neg), t(7)))
}
`,
	"r1_panic_callee": `package main

func div(a, b int) int { return a / b }
func f(a, b, c int) int { return div(a, b) + div(c, b-1) }

func main() {
	println(f(10, 2, 9))
	println(f(10, 1, 9))
}
`,
}

// checkR1Programs runs progs by gc, the interpreter, aether build with and
// without -O0, and ABI-R1: all must agree.
func checkR1Programs(t *testing.T, progs map[string][]byte) {
	requireTools(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	want := gcRun(t, gobin, progs)
	for name, r := range want {
		if q, ok, err := panicPrefix(r); err != nil {
			t.Fatalf("%s: gc output does not fit the panic rule: %v", name, err)
		} else if ok {
			want[name] = result{stderr: q, exit: 2}
		}
	}
	dir := t.TempDir()
	for _, name := range slices.Sorted(maps.Keys(progs)) {
		got, ds, err := interpret(name+".go", progs[name])
		if len(ds) > 0 || err != nil {
			t.Fatalf("%s: not interpreted: %v %v", name, ds, err)
		}
		if got != want[name] {
			t.Errorf("%s: interpreter differs from gc:\n%s", name, diffResults(got, want[name]))
		}
		for _, o0 := range []bool{false, true} {
			exe := filepath.Join(dir, fmt.Sprintf("%s.%v", name, o0))
			if ds, err := build([]source{{name + ".go", progs[name]}}, buildOptions{out: exe, o0: o0}, io.Discard); len(ds) > 0 || err != nil {
				t.Fatalf("%s: -O0 %v: not built: %v %v", name, o0, ds, err)
			}
			if got := runExe(t, exe); got != want[name] {
				t.Errorf("%s: -O0 %v: native differs from gc:\n%s", name, o0, diffResults(got, want[name]))
			}
		}
		if got := runR1(t, name, emitR1(t, name+".go", progs[name]), dir); got != want[name] {
			t.Errorf("%s: ABI-R1 differs from gc:\n%s", name, diffResults(got, want[name]))
		}
	}
}

// TestABIR1Programs checks the convention's own programs, and that the
// permutation program really does exercise every copy cycle.
func TestABIR1Programs(t *testing.T) {
	progs := map[string][]byte{"r1_perm": r1Permutations()}
	for name, src := range r1Programs {
		progs[name] = []byte(src)
	}
	checkR1Programs(t, progs)
	// Coalescing leaves most parameters in their own argument registers, so
	// the permutations are emitted without it, and must run as the default
	// build does.
	prog, ds := compile([]source{{"r1_perm.go", progs["r1_perm"]}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	plain := r1Options
	plain.Coalesce = false
	asm, err := emitProgramWith(prog, plain)
	if err != nil {
		t.Fatal(err)
	}
	requireTools(t)
	dir := t.TempDir()
	if runR1(t, "r1_perm_plain.go", asm, dir) != runR1(t, "r1_perm.go", emitR1(t, "r1_perm.go", progs["r1_perm"]), dir) {
		t.Error("r1_perm: without coalescing differs from the default build")
	}
	cycles := 0
	for _, fn := range strings.Split(string(asm), "\t.globl\t")[1:] {
		if !strings.HasPrefix(fn, "main.p") {
			continue
		}
		_, atCall, _ := strings.Cut(fn, "\tmovq\t%rsp, %rbp\n")
		atCall, _, _ = strings.Cut(atCall, "\tcall\tmain.sink")
		_, moves, _ := strings.Cut(atCall, "= call")
		if strings.Contains(moves, ", %rax\n") {
			cycles++
		}
	}
	if cycles != 719 {
		t.Errorf("%d call sites break a copy cycle, want 719 of the 720 permutations", cycles)
	}
}

// TestABIR1PanicOrder pins D-024's order with panics in arguments, which is not
// gc's (V-4), so without gc: arguments run strictly left to right, and the calls
// after a panicking operand do not run.
func TestABIR1PanicOrder(t *testing.T) {
	requireTools(t)
	for name, c := range map[string]struct{ args, want string }{
		"division": {"t(1), t(2)/zero, t(3)", "1\n2\npanic: runtime error: integer divide by zero\n"},
		"shift":    {"t(1), t(2)<<(zero-1), t(3)", "1\n2\npanic: runtime error: negative shift amount\n"},
		"seventh":  {"t(1), t(2), t(3), t(4), t(5), t(6), t(7)/zero, t(8)", "1\n2\n3\n4\n5\n6\n7\npanic: runtime error: integer divide by zero\n"},
	} {
		sig := "a, b, c int"
		if name == "seventh" {
			sig = "a, b, c, d, e, g, h, i int"
		}
		src := []byte("package main\n\nvar zero int\n\nfunc t(x int) int {\n\tprintln(x)\n\treturn x\n}\n\nfunc f(" + sig + ") int { return a }\n\nfunc main() {\n\tprintln(f(" + c.args + "))\n}\n")
		got, ds, err := interpret(name+".go", src)
		if len(ds) > 0 || err != nil {
			t.Fatalf("%s: not interpreted: %v %v", name, ds, err)
		}
		if !strings.HasPrefix(got.stderr, c.want) || got.exit != 2 {
			t.Errorf("%s: interpreter: exit %d, stderr %q; want exit 2, %q", name, got.exit, got.stderr, c.want)
		}
		if r1 := runR1(t, name, emitR1(t, name+".go", src), t.TempDir()); r1 != got {
			t.Errorf("%s: ABI-R1 differs from the interpreter:\n%s", name, diffResults(r1, got))
		}
	}
}

// TestABIR1SelectPrograms runs TestSelectDifferential's programs under ABI-R1.
func TestABIR1SelectPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the programs of TestSelectDifferential")
	}
	progs := map[string][]byte{}
	for _, ty := range selectTypes {
		progs["sel_"+ty.name] = selectProgram(ty)
	}
	for name, src := range selectFixed {
		progs[name] = []byte(src)
	}
	checkR1Programs(t, progs)
}

// avoidsR1 reports whether asm, ABI-S0 assembly of the default pipeline, has no
// path that ABI-R1 changes: only main.main and main.init, which take and return
// nothing, are Aether functions, and neither is called from the program.
func avoidsR1(asm []byte) bool {
	for _, m := range funcType.FindAllSubmatch(asm, -1) {
		if s := string(m[1]); s != "main.main" && s != "main.init" {
			return false
		}
	}
	return !bytes.Contains(asm, []byte("\tcall\tmain."))
}

// TestABIR1Suite cross-checks ABI-R1 with ABI-S0 on the S2 suite and the
// workloads: each executable must give the interpreter's result, and the
// workloads gc's too. No function's frame may grow, and a program with no path
// that ABI-R1 changes must not change by a byte (D-030 criterion 9).
func TestABIR1Suite(t *testing.T) {
	if testing.Short() {
		t.Skip("builds about 500 executables")
	}
	requireTools(t)
	progs, _ := s2Suite(t)
	gcOut := map[string][]byte{}
	for _, w := range benchWorkloads(t) {
		progs = append(progs, s2Program{w.name + ".go", w.src})
		gcOut[w.name+".go"] = w.want
	}
	dir := t.TempDir()
	smaller, identical := 0, 0
	for _, p := range progs {
		want := result{stderr: string(gcOut[p.name])}
		if gcOut[p.name] == nil {
			var ds []diag.Diagnostic
			var err error
			if want, ds, err = interpret(p.name, p.src); len(ds) > 0 || err != nil {
				t.Fatalf("%s: not interpreted: %v %v", p.name, ds, err)
			}
		}
		prog, ds := compile([]source{{p.name, p.src}})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		s0, err := emitProgramWith(prog, s0Options)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		r1 := emitR1(t, p.name, p.src)
		def, ds := compile([]source{{p.name, p.src}})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		if asm, err := emitProgram(def); err != nil || !bytes.Equal(asm, r1) {
			t.Fatalf("%s: the default build is not ABI-R1 (%v)", p.name, err)
		}
		exe := filepath.Join(dir, strings.TrimSuffix(p.name, ".go")+".s0")
		if ds, err := assemble(s0, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("%s: %v %v", p.name, ds, err)
		}
		if got := runExe(t, exe); got != want {
			t.Errorf("%s: ABI-S0 differs:\n%s", p.name, diffResults(got, want))
		}
		if got := runR1(t, p.name, r1, dir); got != want {
			t.Errorf("%s: ABI-R1 differs:\n%s", p.name, diffResults(got, want))
		}
		sc, sStack := measureAsm(t, s0)
		rc, rStack := measureAsm(t, r1)
		if rStack > sStack {
			t.Errorf("%s: aether.mainstack %d, ABI-S0 %d", p.name, rStack, sStack)
		}
		for name, c := range rc {
			if c.frame > sc[name].frame {
				t.Errorf("%s: %s: frame of %d bytes, ABI-S0 %d", p.name, name, c.frame, sc[name].frame)
			} else if c.frame < sc[name].frame {
				smaller++
			}
		}
		if avoidsR1(s0) {
			identical++
			if !bytes.Equal(s0, r1) {
				t.Errorf("%s: no path ABI-R1 changes, but its assembly differs", p.name)
			}
		}
		if base := strings.TrimSuffix(p.name, ".go"); (base == "b03_collatz" || base == "b04_matpow") && !avoidsR1(s0) {
			t.Logf("%s now has a path ABI-R1 changes: semantic equivalence applies, not byte-identity", base)
		}
	}
	t.Logf("%d programs: %d functions with smaller frames, %d programs byte-identical", len(progs), smaller, identical)
}

// TestABIR1Limits builds functions over each limit on emitProgram under ABI-R1,
// with parameters: over the allocator's limit and over the limit on promotion.
func TestABIR1Limits(t *testing.T) {
	requireTools(t)
	dir := t.TempDir()
	for _, c := range []struct {
		name string
		n    int
		ssa  bool
	}{
		{"over the allocator's limit", 3000, true},
		{"over the limit on promotion", maxPromoteInstrs + 1, false},
	} {
		src := "package main\n\nfunc f(a, b, c, d, e, g, h, i int) (int, int) {\n\tprintln(" + strings.Repeat("0, ", c.n-1) + "a)\n\treturn i - h, g + e\n}\n\nfunc one(a, b int) int {\n\tprintln(" + strings.Repeat("0, ", c.n-1) + "b)\n\treturn a\n}\n\nfunc main() {\n\tx, y := f(1, 2, 3, 4, 5, 6, 7, 8)\n\tprintln(x, y, one(9, 10))\n}\n"
		prog, ds := compile([]source{{"p.go", []byte(src)}})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		asm, err := emitProgramWith(prog, r1Options)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, f := range prog.Funcs {
			if f.Name == "f" && f.SSA != c.ssa {
				t.Errorf("%s: f.SSA = %v, want %v", c.name, f.SSA, c.ssa)
			}
		}
		line := func(last int) string { return strings.Repeat("0 ", c.n-1) + fmt.Sprint(last) + "\n" }
		want := result{stderr: line(1) + line(10) + "1 11 9\n"}
		if got := runR1(t, strings.ReplaceAll(c.name, " ", "_"), asm, dir); got != want {
			t.Errorf("%s: exit %d, %d bytes of stderr; want exit 0, %d bytes", c.name, got.exit, len(got.stderr), len(want.stderr))
		}
	}
}
