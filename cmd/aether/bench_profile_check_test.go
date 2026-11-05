package main

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Tests of the profiling instrumentation of bench_profile_test.go. They are
// part of the normal test suite; the benchmarks themselves are not
// (bench_test.go).

func TestBenchSplitOperands(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"%rax", []string{"%rax"}},
		{"$1, %rax", []string{"$1", "%rax"}},
		{"-8(%rbp), %rax", []string{"-8(%rbp)", "%rax"}},
		{"%rax, 16(%rsp)", []string{"%rax", "16(%rsp)"}},
		{"main.g(%rip), %rcx", []string{"main.g(%rip)", "%rcx"}},
		{"(%rax,%rcx,8), %rdx", []string{"(%rax,%rcx,8)", "%rdx"}},
	} {
		if got := splitOperands(c.in); !slices.Equal(got, c.want) {
			t.Errorf("splitOperands(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBenchClassify(t *testing.T) {
	const slots = 2 // -8(%rbp) and -16(%rbp) are slots
	for _, c := range []struct {
		op   string
		want memClass
	}{
		{"%rax", mNone},
		{"$5", mNone},
		{"-8(%rbp)", mSlot},
		{"-16(%rbp)", mSlot},
		{"-24(%rbp)", mValue},
		{"16(%rbp)", mArgIn},
		{"0(%rsp)", mArgOut},
		{"8(%rsp)", mArgOut},
		{"main.g(%rip)", mGlobal},
		{"aether.stacklimit(%rip)", mLimit},
	} {
		got, err := classify(c.op, slots)
		if err != nil || got != c.want {
			t.Errorf("classify(%q) = %v, %v; want %v", c.op, got, err, c.want)
		}
	}
	for _, op := range []string{"(%rax)", "x(%rbp)", "8(%rcx)"} {
		if _, err := classify(op, slots); err == nil {
			t.Errorf("classify(%q) succeeded, want an error", op)
		}
	}
}

func TestBenchAccount(t *testing.T) {
	type counts struct {
		loads, stores                                           map[memClass]int
		movRR, calls, helperCalls, cellMoves, phiCopies, instrs int
	}
	for _, c := range []struct {
		line, irOp string
		want       counts
	}{
		{"\tmovq\t-8(%rbp), %rax", "add", counts{loads: map[memClass]int{mSlot: 1}, cellMoves: 1, instrs: 1}},
		{"\tmovq\t%rax, -32(%rbp)", "add", counts{stores: map[memClass]int{mValue: 1}, cellMoves: 1, instrs: 1}},
		{"\tmovq\t%rax, -32(%rbp)", "jump", counts{stores: map[memClass]int{mValue: 1}, cellMoves: 1, phiCopies: 1, instrs: 1}},
		{"\tmovq\t-32(%rbp), %rax", "branch", counts{loads: map[memClass]int{mValue: 1}, cellMoves: 1, instrs: 1}},
		{"\tmovq\t$7, -8(%rbp)", "const", counts{stores: map[memClass]int{mSlot: 1}, cellMoves: 1, instrs: 1}},
		{"\taddq\t-8(%rbp), %rax", "add", counts{loads: map[memClass]int{mSlot: 1}, instrs: 1}},
		{"\tcmpq\t%rax, -24(%rbp)", "lt", counts{loads: map[memClass]int{mValue: 1}, instrs: 1}},
		{"\tmovq\t%rax, %rcx", "shl", counts{movRR: 1, instrs: 1}},
		{"\tmovq\t16(%rbp), %rax", "param", counts{loads: map[memClass]int{mArgIn: 1}, instrs: 1}},
		{"\tmovq\t%rax, 8(%rsp)", "call", counts{stores: map[memClass]int{mArgOut: 1}, instrs: 1}},
		{"\tmovq\tmain.g(%rip), %rax", "load", counts{loads: map[memClass]int{mGlobal: 1}, instrs: 1}},
		{"\tcmpq\taether.stacklimit(%rip), %rax", "prologue", counts{loads: map[memClass]int{mLimit: 1}, instrs: 1}},
		{"\tpushq\t%rbp", "prologue", counts{stores: map[memClass]int{mStack: 1}, instrs: 1}},
		{"\tpopq\t%rbp", "ret", counts{loads: map[memClass]int{mStack: 1}, instrs: 1}},
		{"\tret", "ret", counts{loads: map[memClass]int{mStack: 1}, instrs: 1}},
		{"\tcall\tmain.f", "call", counts{stores: map[memClass]int{mStack: 1}, calls: 1, instrs: 1}},
		{"\tcall\taether.printint", "println", counts{stores: map[memClass]int{mStack: 1}, helperCalls: 1, instrs: 1}},
		{"\tjne\t.L0.b2", "branch", counts{instrs: 1}},
	} {
		r := regionStat{byOp: map[string]int{}, cellMovesByOp: map[string]int{}}
		if err := account(&r, c.line, 2, c.irOp); err != nil {
			t.Errorf("%q: %v", c.line, err)
			continue
		}
		var want regionStat
		for k, n := range c.want.loads {
			want.loads[k] = n
		}
		for k, n := range c.want.stores {
			want.stores[k] = n
		}
		if r.loads != want.loads || r.stores != want.stores || r.movRR != c.want.movRR || r.calls != c.want.calls ||
			r.helperCalls != c.want.helperCalls || r.cellMoves != c.want.cellMoves || r.phiCopies != c.want.phiCopies ||
			r.instrs != c.want.instrs || r.byOp[c.irOp] != 1 || r.cellMovesByOp[c.irOp] != c.want.cellMoves {
			t.Errorf("%q: got %+v", c.line, r)
		}
	}
	for _, line := range []string{"\taddq\t%rax, -8(%rbp)", "\tmovq\t(%rax), %rcx"} {
		r := regionStat{byOp: map[string]int{}, cellMovesByOp: map[string]int{}}
		if err := account(&r, line, 2, "add"); err == nil {
			t.Errorf("account(%q) succeeded, want an error", line)
		}
	}
}

func TestBenchCheckRun(t *testing.T) {
	want := []byte("42\n")
	if err := checkRun([]byte("42\n"), 0, want); err != nil {
		t.Errorf("matching run rejected: %v", err)
	}
	for _, c := range []struct {
		stderr string
		code   int
	}{
		{"42\n", 2},
		{"41\n", 0},
		{"42", 0},
		{"", 0},
		{"42\n42\n", 0},
	} {
		if err := checkRun([]byte(c.stderr), c.code, want); err == nil {
			t.Errorf("checkRun(%q, %d) accepted a wrong run", c.stderr, c.code)
		}
	}
}

// TestBenchInstrumentOnlyInserts checks that instrumenting leaves every
// line of Emit's output in place and in order: the instrumented assembly
// differs only by inserted lines, the rename of main.main, the larger
// main stack, and the appended wrapper.
func TestBenchInstrumentOnlyInserts(t *testing.T) {
	for _, p := range sampleSuite(t, 6) {
		for _, v := range compiledVariants {
			prog, asm := benchEmit(t, p.name, p.src, v)
			ins, err := instrument(asm, prog)
			if err != nil {
				t.Fatalf("%s: %v", p.name, err)
			}
			got := strings.Split(string(ins.asm), "\n")
			incs := 0
			for _, l := range got {
				if strings.HasPrefix(l, "\tincq\taether_prof+") {
					incs++
				}
			}
			if incs != len(ins.regions) {
				t.Errorf("%s: %d counters for %d regions", p.name, incs, len(ins.regions))
			}
			_, stack := measureAsm(t, asm)
			_, stack2 := measureAsm(t, ins.asm)
			if stack2 != stack+16 {
				t.Errorf("%s: main stack %d, want %d", p.name, stack2, stack+16)
			}
			j := 0
			for _, l := range strings.Split(string(asm), "\n") {
				l = reMainMain.ReplaceAllString(l, "main.mainprof")
				if strings.HasPrefix(l, "\t.quad\t") && j > 0 && got[j-1] == "aether.mainstack:" {
					l = fmt.Sprintf("\t.quad\t%d", stack+16)
				}
				for j < len(got) && got[j] != l {
					if !inserted(got[j]) {
						t.Fatalf("%s: line %d %q of the instrumented assembly is neither Emit's nor inserted", p.name, j+1, got[j])
					}
					j++
				}
				if j == len(got) {
					t.Fatalf("%s: line %q of Emit's output missing", p.name, l)
				}
				j++
			}
		}
	}
}

// TestBenchInstrumentedRuns checks that instrumentation does not change
// what programs do: on a sample of the S2 suite, the instrumented
// executable gives the same stderr and exit status as the plain one,
// for every compiled variant, including programs that panic. When the
// program exits normally, the wrapper writes the peak stack and one
// counter per region to stdout.
func TestBenchInstrumentedRuns(t *testing.T) {
	requireTools(t)
	dir := t.TempDir()
	for i, p := range sampleSuite(t, 10) {
		for _, v := range compiledVariants {
			exe := filepath.Join(dir, fmt.Sprint(i, v))
			prog, asm := benchBuild(t, p.name, p.src, v, exe)
			ins, err := instrument(asm, prog)
			if err != nil {
				t.Fatalf("%s: %v", p.name, err)
			}
			if ds, err := assemble(ins.asm, buildOptions{out: exe + ".prof"}, io.Discard); len(ds) > 0 || err != nil {
				t.Fatalf("%s: %v %v", p.name, ds, err)
			}
			plainOut, plainErr, plainCode, _, _ := benchRun(t, exe)
			out, stderr, code, _, _ := benchRun(t, exe+".prof")
			if code != plainCode || !bytes.Equal(stderr, plainErr) || len(plainOut) != 0 {
				t.Errorf("%s %s: instrumented exit %d stderr %q, plain exit %d stderr %q stdout %q",
					p.name, v, code, stderr, plainCode, plainErr, plainOut)
			}
			if code == 0 && len(out) != 8*(len(ins.regions)+1) {
				t.Errorf("%s %s: %d bytes of counters, want %d", p.name, v, len(out), 8*(len(ins.regions)+1))
			}
		}
	}
}

// sampleSuite returns every nth program of the S2 suite.
func sampleSuite(t *testing.T, n int) []s2Program {
	t.Helper()
	progs, _ := s2Suite(t)
	var out []s2Program
	for i := 0; i < len(progs); i += n {
		out = append(out, progs[i])
	}
	return out
}

// inserted reports whether line is one instrument inserts before the wrapper.
func inserted(line string) bool {
	switch line {
	case "\tcmpq\taether_minrsp(%rip), %rsp", "\tjae\t9f", "\tmovq\t%rsp, aether_minrsp(%rip)", "9:":
		return true
	}
	return strings.HasPrefix(line, "\tincq\taether_prof+")
}

// TestBenchProfileCounts checks the measured counts of small programs
// against counts known from their source, for every compiled variant.
func TestBenchProfileCounts(t *testing.T) {
	requireTools(t)
	const src = `package main

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	s := 0
	for i := 0; i < 1000; i++ {
		s += i
	}
	println(s, fib(15))
}
`
	dir := t.TempDir()
	for _, v := range compiledVariants {
		exe := filepath.Join(dir, fmt.Sprint("fib", v))
		prog, asm := benchBuild(t, "fib.go", []byte(src), v, exe)
		d, ins, counts := profile(t, prog, asm, exe+".prof", []byte("499500 610\n"))
		// fib(15) makes 2·fib(16)−1 = 1973 calls, all of them by a call
		// instruction in main.main or main.fib.
		if d.calls != 1973 {
			t.Errorf("%s: %d calls, want 1973", v, d.calls)
		}
		freq := blockFreq(prog, ins, counts)
		if got := freq["main.fib"][0]; got != 1973 {
			t.Errorf("%s: fib's entry block ran %d times, want 1973", v, got)
		}
		// fib(15) reaches 987 leaves, where n < 2; the other 986 calls
		// fall through the conditional jump, which starts a region of
		// its own that no label starts.
		fallThrough := false
		for i, r := range ins.regions {
			if r.fn == "main.fib" && r.block < 0 && counts[i] == 986 {
				fallThrough = true
			}
		}
		if !fallThrough {
			t.Errorf("%s: no region of fib ran 986 times after its conditional jump", v)
		}
		m := freq["main.main"]
		if m[0] != 1 || !slices.Contains(m, 1001) || !slices.Contains(m, 1000) {
			t.Errorf("%s: main's block counts %v, want 1 for the entry, 1001 for the loop header, 1000 for the body", v, m)
		}
		// Every instruction counted is in a region; summing per function
		// gives the program's total.
		var sum int64
		for _, fd := range d.byFunc {
			sum += fd.instrs
		}
		if sum != d.instrs || d.instrs == 0 {
			t.Errorf("%s: per-function instructions sum to %d, total %d", v, sum, d.instrs)
		}
	}
}

// TestBenchPeakStack checks the stack probe: with no calls, the peak is
// main's frame plus the wrapper's alignment word, its return address, and
// main's saved %rbp; each extra level of recursion adds the callee's frame,
// its return address, and its saved %rbp.
func TestBenchPeakStack(t *testing.T) {
	requireTools(t)
	dir := t.TempDir()
	peak := func(name, src string) (int64, map[string]funcCode) {
		exe := filepath.Join(dir, name)
		prog, asm := benchBuild(t, name+".go", []byte(src), "slot", exe)
		d, _, _ := profile(t, prog, asm, exe+".prof", []byte("0\n"))
		code, _ := measureAsm(t, asm)
		return d.peakStack, code
	}
	p0, code := peak("flat", "package main\n\nfunc main() { println(0) }\n")
	if want := int64(8 + 8 + 8 + code["main.main"].frame); p0 != want {
		t.Errorf("peak stack without calls %d, want %d", p0, want)
	}
	rec := func(n int) string {
		return fmt.Sprintf(`package main

func down(n int) int {
	if n == 0 {
		return 0
	}
	return down(n-1)
}

func main() { println(down(%d)) }
`, n)
	}
	p10, code := peak("rec10", rec(10))
	p110, _ := peak("rec110", rec(110))
	if want := int64(100 * (code["main.down"].frame + 16)); p110-p10 != want {
		t.Errorf("100 more levels of recursion add %d bytes of stack, want %d", p110-p10, want)
	}
}
