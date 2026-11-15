package main

// Aggregate benchmarks (Phase 6 increment 6.3). The workloads are
// testdata/bench_agg/*.go, each with name.gcout, the stderr of its gc-built
// binary. TestBenchAggWorkloads checks them in the normal suite;
// TestBenchAgg measures them only when AETHER_BENCH_OUT is set:
//
//	AETHER_BENCH_OUT=dir            executables and reports go here
//	AETHER_BENCH_RUNS=n             timed runs per executable (default 15)
//	AETHER_BENCH_COMPILE_RUNS=n     timed builds per compiler (default 7)
//
// It builds each workload with gc (GOAMD64=v1), aether build, and aether
// build -O0, timing each build as a whole process, then times the
// executables round-robin, and profiles the aether build executable to
// attribute its executed instructions to the IR operations that emitted
// them (bench_profile_test.go). It writes agg_compile.tsv, agg_times.tsv,
// agg_summary.tsv, agg_profile.tsv, and environment.txt.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// aggWorkloads returns the workloads of testdata/bench_agg in name order.
func aggWorkloads(t *testing.T) []benchWorkload { return workloadsIn(t, "bench_agg") }

// workloadsIn returns the workloads of testdata/dir, each name.go with
// name.gcout, in name order.
func workloadsIn(t *testing.T, dir string) []benchWorkload {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workloads in testdata/%s: %v", dir, err)
	}
	sort.Strings(files)
	var ws []benchWorkload
	for _, path := range files {
		w := benchWorkload{name: strings.TrimSuffix(filepath.Base(path), ".go")}
		if w.src, err = os.ReadFile(path); err != nil {
			t.Fatal(err)
		}
		if w.want, err = os.ReadFile(strings.TrimSuffix(path, ".go") + ".gcout"); err != nil {
			t.Fatal(err)
		}
		ws = append(ws, w)
	}
	return ws
}

// TestBenchAggWorkloads checks, in the normal suite, that every aggregate
// workload is inside the supported subset and has a recorded gc output.
func TestBenchAggWorkloads(t *testing.T) {
	for _, w := range aggWorkloads(t) {
		if _, ds := compile([]source{{w.name + ".go", w.src}}); len(ds) > 0 {
			t.Errorf("%s: %v", w.name, ds)
		}
		if len(w.want) == 0 || w.want[len(w.want)-1] != '\n' {
			t.Errorf("%s: recorded output %q is empty or unterminated", w.name, w.want)
		}
	}
}

// aggVariants are the builds TestBenchAgg compares.
var aggVariants = []string{"aether", "O0", "gc"}

func envRuns(t *testing.T, name string, def int) int {
	t.Helper()
	s := os.Getenv(name)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 2 {
		t.Fatalf("%s=%q: want an integer of at least 2", name, s)
	}
	return n
}

func TestBenchAgg(t *testing.T) {
	benchSuite(t, "bench_agg", "agg", []string{"addr", "ldm", "stm"})
}

// benchSuite measures the workloads of testdata/dir as TestBenchAgg
// describes, writing prefix_compile.tsv and the other reports, and
// profiles the share of the executed instructions that come from the IR
// operations ops.
func benchSuite(t *testing.T, dir, prefix string, ops []string) {
	out := benchOut(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not found")
	}
	runs := envRuns(t, "AETHER_BENCH_RUNS", 15)
	compileRuns := envRuns(t, "AETHER_BENCH_COMPILE_RUNS", 7)
	aether := filepath.Join(out, "aether")
	if o, err := exec.Command(gobin, "build", "-o", aether, ".").CombinedOutput(); err != nil {
		t.Fatalf("building aether: %v\n%s", err, o)
	}
	ws := workloadsIn(t, dir)

	// Builds: each command once to warm caches, then compileRuns timed.
	var comp strings.Builder
	comp.WriteString("workload\tvariant\trun\twall ns\n")
	compTimes := map[[2]string][]float64{}
	for _, w := range ws {
		src := filepath.Join(out, w.name+".go")
		writeReport(t, out, w.name+".go", string(w.src))
		mod := filepath.Join(out, w.name+".mod")
		if err := os.MkdirAll(mod, 0o755); err != nil {
			t.Fatal(err)
		}
		writeReport(t, mod, "go.mod", "module bench\n\ngo 1.26\n")
		writeReport(t, mod, "main.go", string(w.src))
		mk := func(v string) *exec.Cmd {
			exe := filepath.Join(out, w.name+"."+v)
			var c *exec.Cmd
			switch v {
			case "aether":
				c = exec.Command(aether, "build", "-o", exe, src)
			case "O0":
				c = exec.Command(aether, "build", "-O0", "-o", exe, src)
			default:
				c = exec.Command(gobin, "build", "-o", exe, ".")
				c.Dir = mod
				c.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=", "GOAMD64=v1")
			}
			return c
		}
		for r := -1; r < compileRuns; r++ {
			for _, v := range aggVariants {
				c := mk(v)
				t0 := time.Now()
				if o, err := c.CombinedOutput(); err != nil {
					t.Fatalf("%s %s: %v\n%s", w.name, v, err, o)
				}
				d := time.Since(t0)
				if r >= 0 {
					fmt.Fprintf(&comp, "%s\t%s\t%d\t%d\n", w.name, v, r, d.Nanoseconds())
					k := [2]string{w.name, v}
					compTimes[k] = append(compTimes[k], float64(d.Nanoseconds())/1e6)
				}
			}
		}
	}
	writeReport(t, out, prefix+"_compile.tsv", comp.String())

	// Executions, round-robin, after one discarded warm-up run each.
	var raw strings.Builder
	raw.WriteString("workload\tvariant\trun\twall ns\tcpu ns\n")
	cpu, wall := map[[2]string][]float64{}, map[[2]string][]float64{}
	for _, w := range ws {
		for r := -1; r < runs; r++ {
			for _, v := range aggVariants {
				exe := filepath.Join(out, w.name+"."+v)
				_, stderr, code, wl, c := benchRun(t, exe)
				if err := checkRun(stderr, code, w.want); err != nil {
					t.Fatalf("%s: %v", exe, err)
				}
				if r >= 0 {
					fmt.Fprintf(&raw, "%s\t%s\t%d\t%d\t%d\n", w.name, v, r, wl.Nanoseconds(), c.Nanoseconds())
					k := [2]string{w.name, v}
					cpu[k] = append(cpu[k], float64(c.Nanoseconds())/1e6)
					wall[k] = append(wall[k], float64(wl.Nanoseconds())/1e6)
				}
			}
		}
	}
	writeReport(t, out, prefix+"_times.tsv", raw.String())

	// Where aether build's executed instructions come from: the IR
	// operations ops (for aggregates, those of memory objects: addr,
	// ldm, stm) against the rest.
	var prof strings.Builder
	prof.WriteString("workload\tdyn instrs\t" + strings.Join(ops, "+") + "\tshare\tdata memory operands\ttop ops\n")
	for _, w := range ws {
		exe := filepath.Join(out, w.name+".prof")
		prog, asm := benchEmit(t, w.name+".go", w.src, "copy")
		d, _, _ := profile(t, prog, asm, exe, w.want)
		var mem int64
		for _, op := range ops {
			mem += d.byOp[op]
		}
		type opCount struct {
			op string
			n  int64
		}
		var ops []opCount
		for op, n := range d.byOp {
			ops = append(ops, opCount{op, n})
		}
		sort.Slice(ops, func(i, j int) bool { return ops[i].n > ops[j].n || ops[i].n == ops[j].n && ops[i].op < ops[j].op })
		var top []string
		for _, o := range ops[:min(5, len(ops))] {
			top = append(top, fmt.Sprintf("%s %s", o.op, pct(o.n, d.instrs)))
		}
		fmt.Fprintf(&prof, "%s\t%d\t%d\t%s\t%d\t%s\n", w.name, d.instrs, mem, pct(mem, d.instrs),
			d.loads[mData]+d.stores[mData], strings.Join(top, ", "))
	}
	writeReport(t, out, prefix+"_profile.tsv", prof.String())

	var sum strings.Builder
	sum.WriteString("workload\tvariant\tn\tcpu median ms\tcpu IQR\tcpu min-max\twall median ms\tbuild median ms\n")
	for _, w := range ws {
		for _, v := range aggVariants {
			k := [2]string{w.name, v}
			s := summarize(cpu[k], wall[k])
			ct := append([]float64(nil), compTimes[k]...)
			sort.Float64s(ct)
			fmt.Fprintf(&sum, "%s\t%s\t%d\t%.1f\t%.1f-%.1f\t%.1f-%.1f\t%.1f\t%.1f\n", w.name, v, s.n,
				s.cpuMed, s.cpuQ1, s.cpuQ3, s.cpuMin, s.cpuMax, s.wallMed, median(ct))
		}
	}
	sum.WriteString("\nworkload\taether/gc cpu\tO0/gc cpu\tO0/aether cpu\n")
	for _, w := range ws {
		m := func(v string) float64 {
			c := append([]float64(nil), cpu[[2]string{w.name, v}]...)
			sort.Float64s(c)
			return median(c)
		}
		fmt.Fprintf(&sum, "%s\t%.2f\t%.2f\t%.2f\n", w.name, m("aether")/m("gc"), m("O0")/m("gc"), m("O0")/m("aether"))
	}
	writeReport(t, out, prefix+"_summary.tsv", sum.String())
}
