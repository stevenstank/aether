package main

import (
	"bytes"
	"debug/elf"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stevenstank/aether/internal/amd64"
	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/gate"
	"github.com/stevenstank/aether/internal/ir"
	"github.com/stevenstank/aether/internal/lower"
)

// stageNames are the stages of aether build as stageRun splits them: parse;
// typecheck, with the gate's file-level precheck before it; the gate's subset
// check; lower, with the IR verifier; promote; passes (buildPasses); emit,
// with the emitter's own verifier and checkers; and assemble, which finds and
// identifies as and ld and runs them.
var stageNames = [...]string{"parse", "typecheck", "gate", "lower", "promote", "passes", "emit", "assemble"}

const nStages = len(stageNames)

// stageCost is what one build of a program costs, by stage: wall time, bytes
// and objects allocated, and the heap still live after the stage (after a
// collection).
type stageCost struct {
	ns, alloc, mallocs, live [nStages]uint64
}

// stageRun builds the single-file program src into exe with the compiler's own
// functions, a stage at a time, after a collection before each. With mem it
// also reads the allocation counters around each stage, outside the timed
// part. It returns the program and the assembly, which must be what compile
// and emitProgram give: checkStages checks that.
func stageRun(t *testing.T, name string, src []byte, exe string, mem bool) (stageCost, *ir.Program, []byte) {
	t.Helper()
	var c stageCost
	var (
		fset = token.NewFileSet()
		file *ast.File
		pkg  *frontend.Package
		prog *ir.Program
		asm  []byte
	)
	fail := func(stage string, v any) { t.Fatalf("%s: %s: %v", name, stage, v) }
	steps := [nStages]func(){
		func() {
			var ds []diag.Diagnostic
			if file, ds = frontend.Parse(fset, name, src); len(ds) > 0 {
				fail("parse", ds)
			}
		},
		func() {
			if ds := gate.Precheck(fset, file); len(ds) > 0 {
				fail("precheck", ds)
			}
			var ds []diag.Diagnostic
			if pkg, ds = frontend.TypeCheck(fset, []*ast.File{file}); len(ds) > 0 {
				fail("typecheck", ds)
			}
		},
		func() {
			if ds := gate.Check(pkg); len(ds) > 0 {
				fail("gate", ds)
			}
		},
		func() {
			var ds []diag.Diagnostic
			if prog, ds = lower.Lower(pkg); len(ds) > 0 {
				fail("lower", ds)
			}
			if ds := verify(prog, name); len(ds) > 0 {
				fail("verify", ds)
			}
		},
		func() {
			for _, f := range prog.Funcs {
				if promotable(f) {
					if err := ir.Promote(f); err != nil {
						fail("promote", err)
					}
				}
			}
		},
		func() {
			for _, f := range prog.Funcs {
				for _, p := range buildPasses {
					if f.SSA {
						if _, err := p(f); err != nil {
							fail("passes", err)
						}
					}
				}
			}
		},
		func() {
			var err error
			if asm, err = amd64.EmitWith(prog, r1Options); err != nil {
				fail("emit", err)
			}
		},
		func() {
			if ds, err := assemble(asm, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
				fail("assemble", fmt.Sprint(ds, err))
			}
		},
	}
	var before, after runtime.MemStats
	for i, step := range steps {
		runtime.GC()
		if mem {
			runtime.ReadMemStats(&before)
		}
		start := time.Now()
		step()
		c.ns[i] = uint64(time.Since(start).Nanoseconds())
		if mem {
			runtime.ReadMemStats(&after)
			c.alloc[i] = after.TotalAlloc - before.TotalAlloc
			c.mallocs[i] = after.Mallocs - before.Mallocs
			runtime.GC()
			runtime.ReadMemStats(&after)
			c.live[i] = after.HeapAlloc
		}
	}
	runtime.KeepAlive(pkg)
	return c, prog, asm
}

// checkStages checks that stageRun builds what aether build builds: the same
// IR after lowering and the same assembly.
func checkStages(t *testing.T, name string, src []byte) {
	t.Helper()
	_, _, asm := stageRun(t, name, src, filepath.Join(t.TempDir(), "out"), false)
	prog, ds := compile([]source{{name, src}})
	if len(ds) > 0 {
		t.Fatalf("%s: %v", name, ds)
	}
	want, err := emitProgram(prog)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if !bytes.Equal(asm, want) {
		t.Fatalf("%s: the stages build other assembly than emitProgram", name)
	}
}

// peakRSS runs aether build on src, as a process, and returns its peak
// resident set in KiB, not counting as and ld, its children. GNU time (with
// -f %M) measures it: the ru_maxrss of a child the test starts itself would
// include the test process's own peak, since Go starts children sharing the
// parent's address space until exec, and Linux keeps that high-water mark
// across it. GNU time forks first, so its child carries only time's own
// (about 1 MiB), well below any build's.
func peakRSS(t *testing.T, bin, dir, name string, src []byte) int64 {
	t.Helper()
	gnuTime := "/usr/bin/time"
	if _, err := os.Stat(gnuTime); err != nil {
		t.Skip("GNU time not found")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(gnuTime, "-f", "%M", bin, "build", "-o", filepath.Join(dir, "out"), path).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: aether build: %v\n%s", name, err, out)
	}
	kib, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		t.Fatalf("%s: GNU time printed %q, want only the peak RSS", name, out)
	}
	return kib
}

// benchRuns is AETHER_BENCH_RUNS, which must be at least 2.
func benchRuns(t *testing.T) int {
	runs, err := strconv.Atoi(os.Getenv("AETHER_BENCH_RUNS"))
	if err != nil || runs < 2 {
		t.Fatalf("AETHER_BENCH_RUNS=%q: want an integer of at least 2", os.Getenv("AETHER_BENCH_RUNS"))
	}
	return runs
}

// medianCost runs stageRun runs times, after a discarded warm-up, and returns
// the median time of each stage and the allocation of the first measured
// run (one build's allocation varies only with the collector's timing).
func medianCost(t *testing.T, name string, src []byte, exe string, runs int) stageCost {
	t.Helper()
	stageRun(t, name, src, exe, false)
	c, _, _ := stageRun(t, name, src, exe, true)
	times := make([][]float64, nStages)
	for range runs {
		r, _, _ := stageRun(t, name, src, exe, false)
		for i := range nStages {
			times[i] = append(times[i], float64(r.ns[i]))
		}
	}
	for i := range nStages {
		slices.Sort(times[i])
		c.ns[i] = uint64(median(times[i]))
	}
	return c
}

func sum(x [nStages]uint64, from, to int) (s uint64) {
	for _, v := range x[from:to] {
		s += v
	}
	return s
}

func mib(b uint64) string { return fmt.Sprintf("%.2f", float64(b)/(1<<20)) }
func ms(ns uint64) string { return fmt.Sprintf("%.3f", float64(ns)/1e6) }

// TestBenchMemory measures the compiler's memory on the latency programs:
// bytes and objects allocated and the heap live after each stage
// (memory.tsv), and the peak RSS of aether build as a process, the median of
// AETHER_BENCH_RUNS builds (memory_summary.tsv). It runs only when
// AETHER_BENCH_OUT is set.
func TestBenchMemory(t *testing.T) {
	dir := benchOut(t)
	runs := benchRuns(t)
	work := t.TempDir()
	bin := aetherBinary(t)
	var raw, sumr strings.Builder
	raw.WriteString("program\tstage\tallocated bytes\tobjects\tlive heap after\n")
	sumr.WriteString("program\tsource bytes\tallocated MiB\tof which frontend (parse to gate)\tlower\tpromote and passes\temit\tassemble\tpeak RSS MiB\tRSS min\tRSS max\n")
	for _, p := range latencyPrograms(t) {
		checkStages(t, p.name, p.src)
		stageRun(t, p.name, p.src, filepath.Join(work, "out"), false)
		c, _, _ := stageRun(t, p.name, p.src, filepath.Join(work, "out"), true)
		for i, s := range stageNames {
			fmt.Fprintf(&raw, "%s\t%s\t%d\t%d\t%d\n", p.name, s, c.alloc[i], c.mallocs[i], c.live[i])
		}
		var rss []float64
		for range runs {
			rss = append(rss, float64(peakRSS(t, bin, work, p.name, p.src)))
		}
		slices.Sort(rss)
		fmt.Fprintf(&sumr, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%.1f\t%.1f\t%.1f\n", p.name, len(p.src),
			mib(sum(c.alloc, 0, nStages)), mib(sum(c.alloc, 0, 3)), mib(c.alloc[3]), mib(sum(c.alloc, 4, 6)), mib(c.alloc[6]), mib(c.alloc[7]),
			median(rss)/1024, rss[0]/1024, rss[len(rss)-1]/1024)
	}
	writeReport(t, dir, "memory.tsv", raw.String())
	writeReport(t, dir, "memory_summary.tsv", sumr.String())
}

// scaleAxes generate S2 programs of size n along one dimension each:
//   - funcs: n functions, each calling the one before it;
//   - stmts: one function of n assignments over four variables;
//   - nest: n nested ifs, each declaring a variable, which grows blocks and
//     slots together.
//
// The values start from a global, so that constant folding cannot remove the
// code.
var scaleAxes = []struct {
	name string
	gen  func(n int) string
}{
	{"funcs", func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\nfunc f0(x int) int { return x + 1 }\n")
		for k := 1; k < n; k++ {
			fmt.Fprintf(&b, "\nfunc f%d(x int) int { return f%d(x)*3 ^ %d }\n", k, k-1, k)
		}
		fmt.Fprintf(&b, "\nfunc main() { println(f%d(1)) }\n", n-1)
		return b.String()
	}},
	{"stmts", func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\nvar seed = 1\n\nfunc main() {\n\ta, b, c, d := seed, seed+1, seed+2, seed+3\n")
		forms := []string{"a = a*3 + b", "b = b ^ c", "c = c - d*5", "d = d + a"}
		for k := range n {
			fmt.Fprintf(&b, "\t%s\n", forms[k%len(forms)])
		}
		b.WriteString("\tprintln(a, b, c, d)\n}\n")
		return b.String()
	}},
	{"nest", func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\nvar seed = 0\n\nfunc main() {\n\tx := seed\n")
		for k := range n {
			fmt.Fprintf(&b, "if x < %d {\nv%d := x + %d\nx = v%d * 2 %% 1000\n", k+1, k, k, k)
		}
		b.WriteString(strings.Repeat("}\n", n))
		b.WriteString("\tprintln(x)\n}\n")
		return b.String()
	}},
}

// adversarialAxes generate programs aimed at the analyses that once grew
// faster than the program:
//   - wide: n variables declared first and read after a run of n ifs, so
//     every variable is live across every block;
//   - loops: n nested for loops around one assignment, so liveness must
//     cross n back edges;
//   - cases: a switch of n cases in a loop, each assigning two variables, so
//     one block has n predecessors and φs with n edges.
var adversarialAxes = []struct {
	name string
	gen  func(n int) string
}{
	{"wide", func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\nvar seed = 1\n\nfunc main() {\n\tx := seed\n")
		for k := range n {
			fmt.Fprintf(&b, "\tv%d := seed + %d\n", k, k)
		}
		for k := range n {
			fmt.Fprintf(&b, "\tif x > %d {\n\t\tx = x + v%d\n\t}\n", k, k)
		}
		b.WriteString("\ts := 0\n")
		for k := range n {
			fmt.Fprintf(&b, "\ts = s ^ v%d\n", k)
		}
		b.WriteString("\tprintln(x, s)\n}\n")
		return b.String()
	}},
	{"loops", func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\nvar seed = 1\n\nfunc main() {\n\tx := seed\n")
		for k := range n {
			fmt.Fprintf(&b, "for i%d := 0; i%d < 1; i%d++ {\n", k, k, k)
		}
		b.WriteString("x = x*3 + 1\n")
		b.WriteString(strings.Repeat("}\n", n))
		b.WriteString("\tprintln(x)\n}\n")
		return b.String()
	}},
	{"cases", func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\nvar seed = 1\n\nfunc main() {\n\tx, y := seed, seed\n\tfor k := 0; k < 3; k++ {\n\t\tswitch (x + k) % " + strconv.Itoa(n) + " {\n")
		for k := range n {
			fmt.Fprintf(&b, "\t\tcase %d:\n\t\t\tx = x*3 + %d\n\t\t\ty = y + x\n", k, k)
		}
		b.WriteString("\t\t}\n\t}\n\tprintln(x, y)\n}\n")
		return b.String()
	}},
}

// scaleSizes are the sizes of every axis: 16 to 4096, doubling.
var scaleSizes = []int{16, 32, 64, 128, 256, 512, 1024, 2048, 4096}

// TestBenchScale measures compile time and memory against program size, on
// the programs of scaleAxes at scaleSizes. Each program must also run as the
// interpreter runs it. It writes scale.tsv, one row per program, with the
// median of AETHER_BENCH_RUNS builds of each stage, the allocation of one,
// and the peak RSS of aether build (one process); and scale_slopes.tsv, the
// growth exponent between consecutive sizes: log2 of the ratio of a cost at
// 2n to its cost at n, so 1 is linear and 2 quadratic. It runs only when
// AETHER_BENCH_OUT is set.
func TestBenchScale(t *testing.T) {
	dir := benchOut(t)
	runs := benchRuns(t)
	work := t.TempDir()
	bin := aetherBinary(t)
	exe := filepath.Join(work, "out")
	var rows, slopes strings.Builder
	rows.WriteString("axis\tn\tsource bytes\tIR instrs\tlargest func instrs\tfuncs promoted\tfuncs\tparse ms\ttypecheck ms\tgate ms\tlower ms\tpromote ms\tpasses ms\temit ms\tassemble ms\ttotal ms\tallocated MiB\tpeak live MiB\tpeak RSS MiB\ttext bytes\n")
	slopes.WriteString("axis\tfrom n\tto n\ttotal time\tcompiler time (no assemble)\tallocated\tpeak RSS\n")
	for _, ax := range append(slices.Clone(scaleAxes), adversarialAxes...) {
		var prev [4]float64
		for i, n := range scaleSizes {
			name := fmt.Sprintf("%s%d.go", ax.name, n)
			src := []byte(ax.gen(n))
			checkStages(t, name, src)
			c := medianCost(t, name, src, exe, runs)
			_, prog, _ := stageRun(t, name, src, exe, false)
			want, ds, err := interpret(name, src)
			if len(ds) > 0 || err != nil {
				t.Fatalf("%s: %v %v", name, ds, err)
			}
			if got := runExe(t, exe); got != want {
				t.Fatalf("%s: native differs from the interpreter:\n%s", name, diffResults(got, want))
			}
			ef, err := elf.Open(exe)
			if err != nil {
				t.Fatal(err)
			}
			text := ef.Section(".text").Size
			ef.Close()
			promoted := 0
			for _, f := range prog.Funcs {
				if f.SSA {
					promoted++
				}
			}
			// The lowered IR's size, before promotion removes loads and
			// stores, is what the promotion limits see.
			lowered, _ := compile([]source{{name, src}})
			instrs, largest := 0, 0
			for _, f := range lowered.Funcs {
				k := 0
				for _, b := range f.Blocks {
					k += len(b.Instrs)
				}
				instrs += k
				largest = max(largest, k)
			}
			rss := peakRSS(t, bin, work, name, src)
			total, compiler := sum(c.ns, 0, nStages), sum(c.ns, 0, nStages-1)
			alloc := sum(c.alloc, 0, nStages)
			fmt.Fprintf(&rows, "%s\t%d\t%d\t%d\t%d\t%d\t%d", ax.name, n, len(src), instrs, largest, promoted, len(prog.Funcs))
			for _, d := range c.ns {
				fmt.Fprintf(&rows, "\t%s", ms(d))
			}
			fmt.Fprintf(&rows, "\t%s\t%s\t%s\t%.1f\t%d\n", ms(total), mib(alloc), mib(slices.Max(c.live[:])), float64(rss)/1024, text)
			cur := [4]float64{float64(total), float64(compiler), float64(alloc), float64(rss)}
			if i > 0 {
				fmt.Fprintf(&slopes, "%s\t%d\t%d", ax.name, scaleSizes[i-1], n)
				for j := range cur {
					fmt.Fprintf(&slopes, "\t%.2f", math.Log2(cur[j]/prev[j]))
				}
				slopes.WriteString("\n")
			}
			prev = cur
		}
	}
	writeReport(t, dir, "scale.tsv", rows.String())
	writeReport(t, dir, "scale_slopes.tsv", slopes.String())
}

// TestBenchStages checks the harness in the normal suite: stageRun builds what
// aether build builds, on the latency programs and the smallest program of
// each scale axis, and those generated programs run as the interpreter runs
// them.
func TestBenchStages(t *testing.T) {
	requireTools(t)
	for _, p := range latencyPrograms(t) {
		checkStages(t, p.name, p.src)
	}
	exe := filepath.Join(t.TempDir(), "out")
	for _, ax := range append(slices.Clone(scaleAxes), adversarialAxes...) {
		name, src := ax.name+".go", []byte(ax.gen(scaleSizes[0]))
		checkStages(t, name, src)
		c, _, _ := stageRun(t, name, src, exe, true)
		if sum(c.alloc, 0, nStages) == 0 || c.live[nStages-1] == 0 {
			t.Errorf("%s: no allocation measured: %+v", name, c)
		}
		want, ds, err := interpret(name, src)
		if len(ds) > 0 || err != nil {
			t.Fatalf("%s: %v %v", name, ds, err)
		}
		if got := runExe(t, exe); got != want {
			t.Errorf("%s: native differs from the interpreter:\n%s", name, diffResults(got, want))
		}
	}
}
