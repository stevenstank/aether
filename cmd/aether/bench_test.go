package main

// The benchmark harness (BENCHMARKS.md). The workloads are
// testdata/bench/*.go, each with name.gcout, the stderr of its gc-built
// binary. The benchmark tests run only when AETHER_BENCH_OUT names a
// directory for their executables and reports, so the normal test suite
// skips them:
//
//	AETHER_BENCH_OUT=dir   executables, assembly, and reports go here
//	AETHER_BENCH_RUNS=n    timed runs per executable (default 15)
//
// TestBenchOracle builds every workload with gc, default and with
// -gcflags=all='-N -l', and checks both against name.gcout. TestBenchMeasure
// builds every workload as each compiled variant (slot-based, promoted to
// SSA, promoted with registers: RA-0, D-026, and with selection too: D-029,
// which is aether build), checks them against
// name.gcout, and profiles them (bench_profile_test.go). It reports dynamic
// and static counts, per-function and per-IR-op counts, register pressure
// weighted by the measured block frequencies (bench_pressure_test.go), and
// a model of the instructions a register allocator would leave.
// TestBenchSuitePressure reports static pressure on the S2 suite.
// TestBenchTime times every executable of the others, round-robin. Every
// test also writes environment.txt. BENCHMARKS.md has the commands.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// benchWorkload is one checked-in workload.
type benchWorkload struct {
	name      string // b01_fib
	src, want []byte // the program and gc's stderr
}

// benchWorkloads returns the workloads of testdata/bench in name order.
func benchWorkloads(t *testing.T) []benchWorkload {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "bench", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workloads in testdata/bench: %v", err)
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

// benchOut returns AETHER_BENCH_OUT, after writing environment.txt there,
// or skips the test.
func benchOut(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("AETHER_BENCH_OUT")
	if dir == "" {
		t.Skip("AETHER_BENCH_OUT not set; benchmarks are not part of the normal test suite")
	}
	requireTools(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeReport(t, dir, "environment.txt", benchEnvironment())
	return dir
}

// benchEnvironment describes the machine and toolchains (BENCHMARKS.md).
func benchEnvironment() string {
	var b strings.Builder
	first := func(name string, args ...string) string {
		out, err := exec.Command(name, args...).Output()
		if err != nil {
			return "unknown (" + err.Error() + ")"
		}
		return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	}
	field := func(file, key string) string {
		data, err := os.ReadFile(file)
		if err != nil {
			return "unknown"
		}
		for _, l := range strings.Split(string(data), "\n") {
			if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == key {
				return strings.TrimSpace(v)
			}
		}
		return "unknown"
	}
	kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	dirty := "clean"
	if out, err := exec.Command("git", "status", "--porcelain").Output(); err != nil {
		dirty = "unknown"
	} else if len(out) > 0 {
		dirty = "modified"
	}
	fmt.Fprintf(&b, "aether commit\t%s (%s)\n", first("git", "rev-parse", "--short", "HEAD"), dirty)
	fmt.Fprintf(&b, "go\t%s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "as\t%s\n", first("as", "--version"))
	fmt.Fprintf(&b, "ld\t%s\n", first("ld", "--version"))
	fmt.Fprintf(&b, "cpu\t%s\n", field("/proc/cpuinfo", "model name"))
	fmt.Fprintf(&b, "usable cpus\t%d\n", runtime.NumCPU())
	fmt.Fprintf(&b, "affinity\t%s\n", field("/proc/self/status", "Cpus_allowed_list"))
	fmt.Fprintf(&b, "kernel\t%s\n", strings.TrimSpace(string(kernel)))
	return b.String()
}

func writeReport(t *testing.T, dir, name, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBenchWorkloads checks, in the normal test suite, that every workload
// is inside the supported subset and has a recorded gc output.
func TestBenchWorkloads(t *testing.T) {
	ws := benchWorkloads(t)
	gcouts, _ := filepath.Glob(filepath.Join("testdata", "bench", "*.gcout"))
	if len(gcouts) != len(ws) {
		t.Errorf("%d .gcout files for %d workloads", len(gcouts), len(ws))
	}
	for _, w := range ws {
		if _, ds := compile([]source{{w.name + ".go", w.src}}); len(ds) > 0 {
			t.Errorf("%s: %v", w.name, ds)
		}
		if len(w.want) == 0 || w.want[len(w.want)-1] != '\n' {
			t.Errorf("%s: recorded output %q is empty or unterminated", w.name, w.want)
		}
	}
}

// TestBenchOracle builds the gc variants of every workload into
// AETHER_BENCH_OUT, with the installed toolchain (go build, never go run:
// D-014), and checks them against the recorded outputs.
func TestBenchOracle(t *testing.T) {
	out := benchOut(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not found")
	}
	ws := benchWorkloads(t)
	progs := map[string][]byte{}
	for _, w := range ws {
		progs[w.name] = w.src
	}
	for _, v := range []struct{ suffix, gcflags string }{{"gc", ""}, {"gcN", "-gcflags=all=-N -l"}} {
		dir := t.TempDir()
		writeReport(t, dir, "go.mod", "module bench\n\ngo 1.26\n")
		for _, w := range ws {
			if err := os.Mkdir(filepath.Join(dir, w.name), 0o755); err != nil {
				t.Fatal(err)
			}
			writeReport(t, filepath.Join(dir, w.name), "main.go", string(w.src))
		}
		args := []string{"build", "-o", filepath.Join(dir, "bin") + string(filepath.Separator)}
		if v.gcflags != "" {
			args = append(args, v.gcflags)
		}
		cmd := exec.Command(gobin, append(args, "./...")...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=")
		if o, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("gc build failed: %v\n%s", err, o)
		}
		for _, w := range ws {
			exe := filepath.Join(out, w.name+"."+v.suffix)
			data, err := os.ReadFile(filepath.Join(dir, "bin", w.name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(exe, data, 0o755); err != nil {
				t.Fatal(err)
			}
			_, stderr, code, _, _ := benchRun(t, exe)
			if err := checkRun(stderr, code, w.want); err != nil {
				t.Errorf("%s: %v", exe, err)
			}
		}
	}
}

func pct(x, y int64) string {
	if y == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(x)/float64(y))
}

// TestBenchMeasure builds, checks, and profiles every workload, slot-based
// and in SSA form, and writes summary.tsv, functions.tsv, pressure.tsv,
// ops.tsv, and model.tsv.
func TestBenchMeasure(t *testing.T) {
	outDir := benchOut(t)
	var sum, fns, pr, ops, model strings.Builder
	fmt.Fprintf(&ops, "workload\tvariant\tir op\tdyn instrs\tdyn cell moves\n")
	fmt.Fprintf(&model, "workload\tslot dyn\tssa dyn\tssa cell moves\tphi copies\ttwo-addr copies\tcall-crossing spill (2x)\tmodel RA dyn\tmodel/ssa\tmodel/slot\tssa abi instrs (prologue+call+param+ret)\tabi share of ssa\tfrac dyn instrs live>10\tlive>12\n")
	fmt.Fprintf(&sum, "workload\tvariant\tdyn instrs\tdyn mem ops\tcell ld\tcell st\tabi mem\tglobal mem\treg-reg mov\tcalls\tpeak stack\ttext bytes\tstatic instrs\tframe bytes\n")
	fmt.Fprintf(&fns, "workload\tvariant\tfunc\tdyn instrs\tdyn mem\tdyn cell\tdyn abi\tshare of program instrs\n")
	fmt.Fprintf(&pr, "workload\tfunc\tvalues\tphis\tmaxLive\tmaxLive(noconst)\tcrossing\tcrossing(noconst)\tmaxAcrossCall\tcallsDyn\tcrossDyn/call\tdyn frac live>6\t>8\t>10\t>12\t>14\n")
	for _, w := range benchWorkloads(t) {
		name := w.name
		total := make([]int64, len(compiledVariants))
		for vi, variant := range compiledVariants {
			exe := filepath.Join(outDir, name+"."+variant)
			prog, asm := benchBuild(t, name+".go", w.src, variant, exe)
			_, stderr, code, _, _ := benchRun(t, exe)
			if err := checkRun(stderr, code, w.want); err != nil {
				t.Fatalf("%s %s: %v", name, variant, err)
			}
			d, ins, counts := profile(t, prog, asm, exe+".prof", w.want)
			total[vi] = d.instrs
			for _, op := range slices.Sorted(maps.Keys(d.byOp)) {
				fmt.Fprintf(&ops, "%s\t%s\t%s\t%d\t%d\n", name, variant, op, d.byOp[op], d.cellMovesByOp[op])
			}
			static, _ := measureAsm(t, asm)
			var sInstrs, sFrame, sBytes int
			for _, c := range static {
				sInstrs += c.instrs
				sFrame += c.frame
			}
			for _, n := range funcBytes(t, exe) {
				sBytes += n
			}
			fmt.Fprintf(&sum, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", name, variant, d.instrs, d.mem(),
				d.loads[mSlot]+d.loads[mValue], d.stores[mSlot]+d.stores[mValue], d.abi(),
				d.loads[mGlobal]+d.stores[mGlobal], d.movRR, d.calls, d.peakStack, sBytes, sInstrs, sFrame)
			for _, fn := range slices.Sorted(maps.Keys(d.byFunc)) {
				fd := d.byFunc[fn]
				if fd.instrs == 0 {
					continue
				}
				fmt.Fprintf(&fns, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%s\n", name, variant, fn, fd.instrs, fd.mem(), fd.cell(), fd.abi(), pct(fd.instrs, d.instrs))
			}
			if variant == "ssa" {
				writeModel(&model, &pr, name, prog, d, blockFreq(prog, ins, counts), total[0])
			}
		}
		t.Logf("%s: dynamic instructions %v of %v", name, total, compiledVariants)
	}
	for file, s := range map[string]*strings.Builder{"summary.tsv": &sum, "functions.tsv": &fns, "pressure.tsv": &pr, "ops.tsv": &ops, "model.tsv": &model} {
		writeReport(t, outDir, file, s.String())
	}
}

// writeModel writes the pressure of every function of the SSA program prog
// to pr, weighted by freqs, and its line of the model to model. d is the
// SSA program's profile and slotInstrs the slot-based program's dynamic
// instructions.
//
// The model estimates the dynamic instructions left if every value not
// live across a call were in a register: SSA's, minus every move between a
// cell and a register or immediate, plus one move per φ copy, the
// two-address and fixed-register copies (copies), and a store and a load
// for each value live across each executed call. It is an estimate, not a
// measurement.
func writeModel(model, pr *strings.Builder, name string, prog *ir.Program, d dynStats, freqs map[string][]int64, slotInstrs int64) {
	var twoAddr, cross, execs, over10, over12 int64
	for _, f := range prog.Funcs {
		p := analyze(f, freqs["main."+f.Name])
		twoAddr += p.twoAddrDyn
		cross += p.callCrossDyn
		execs += p.instrExecs
		above := func(k int) (n int64) {
			for l, c := range p.hist {
				if l > k {
					n += c
				}
			}
			return n
		}
		over10 += above(10)
		over12 += above(12)
		per := "n/a"
		if p.callsDyn > 0 {
			per = fmt.Sprintf("%.2f", float64(p.callCrossDyn)/float64(p.callsDyn))
		}
		fmt.Fprintf(pr, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n", name, f.Name, p.values, p.phis,
			p.maxLive, p.maxLiveNC, p.crossing, p.crossingNC, p.maxAcrossCall, p.callsDyn, per,
			pct(above(6), p.instrExecs), pct(above(8), p.instrExecs), pct(above(10), p.instrExecs),
			pct(above(12), p.instrExecs), pct(above(14), p.instrExecs))
	}
	abi := d.byOp["prologue"] + d.byOp["call"] + d.byOp["param"] + d.byOp["ret"]
	est := d.instrs - d.cellMoves + d.phiCopies + twoAddr + 2*cross
	fmt.Fprintf(model, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%.3f\t%.3f\t%d\t%s\t%s\t%s\n", name, slotInstrs, d.instrs, d.cellMoves,
		d.phiCopies, twoAddr, 2*cross, est, float64(est)/float64(d.instrs), float64(est)/float64(slotInstrs),
		abi, pct(abi, d.instrs), pct(over10, execs), pct(over12, execs))
}

// TestBenchSuitePressure writes suite_pressure.tsv: the static register
// pressure of every function of the S2 suite in SSA form.
func TestBenchSuitePressure(t *testing.T) {
	outDir := benchOut(t)
	progs, _ := s2Suite(t)
	var rep strings.Builder
	fmt.Fprintf(&rep, "program\tfunc\tvalues\tphis\tmaxLive\tmaxLive(noconst)\tcrossing(noconst)\tmaxAcrossCall\thasCall\n")
	for _, p := range progs {
		prog, ds := compile([]source{{p.name, p.src}})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		if err := promoteProgram(prog, &promoteStats{}); err != nil {
			t.Fatal(err)
		}
		for _, f := range prog.Funcs {
			a := analyze(f, nil)
			hasCall := 0
			for _, b := range f.Blocks {
				for _, in := range b.Instrs {
					if in.Op == ir.Call {
						hasCall = 1
					}
				}
			}
			fmt.Fprintf(&rep, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", p.name, f.Name, a.values, a.phis, a.maxLive, a.maxLiveNC, a.crossingNC, a.maxAcrossCall, hasCall)
		}
	}
	writeReport(t, outDir, "suite_pressure.tsv", rep.String())
}

// TestBenchTime times the executables name.slot, name.ssa, name.gc, and
// name.gcN in AETHER_BENCH_OUT, where present, round-robin: one discarded
// warm-up run of each, then AETHER_BENCH_RUNS runs of each. Every run must
// give the recorded output. It writes the raw times to times.tsv and their
// summary (timesSummary) to times_summary.tsv.
func TestBenchTime(t *testing.T) {
	outDir := benchOut(t)
	runs := 15
	if s := os.Getenv("AETHER_BENCH_RUNS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 2 {
			t.Fatalf("AETHER_BENCH_RUNS=%q: want an integer of at least 2", s)
		}
		runs = n
	}
	var raw strings.Builder
	raw.WriteString("workload\tvariant\trun\twall ns\tcpu ns\n")
	for _, w := range benchWorkloads(t) {
		var exes, variants []string
		for _, v := range benchVariants {
			exe := filepath.Join(outDir, w.name+"."+v)
			if _, err := os.Stat(exe); err == nil {
				exes = append(exes, exe)
				variants = append(variants, v)
			} else {
				t.Logf("%s: not timed: %v", exe, err)
			}
		}
		for r := -1; r < runs; r++ {
			for i, exe := range exes {
				_, stderr, code, wall, cpu := benchRun(t, exe)
				if err := checkRun(stderr, code, w.want); err != nil {
					t.Fatalf("%s: %v", exe, err)
				}
				if r >= 0 {
					fmt.Fprintf(&raw, "%s\t%s\t%d\t%d\t%d\n", w.name, variants[i], r, wall.Nanoseconds(), cpu.Nanoseconds())
				}
			}
		}
	}
	writeReport(t, outDir, "times.tsv", raw.String())
	sum, err := timesSummary(raw.String())
	if err != nil {
		t.Fatal(err)
	}
	writeReport(t, outDir, "times_summary.tsv", sum)
}
