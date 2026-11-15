package main

// Heap benchmarks (Phase 7 increment 7.1): the memory growth of the
// non-collecting allocator (D-010). The workloads are
// testdata/bench_heap/*.go, each with name.gcout, the stderr of its
// gc-built binary. TestBenchHeapWorkloads checks them in the normal suite;
// TestBenchHeap measures them only when AETHER_BENCH_OUT is set, with
// AETHER_BENCH_RUNS runs per executable (default 7).
//
// It builds each workload with aether build, aether build -O0, and gc
// (GOAMD64=v1), and records the peak resident set of each run. GNU time
// measures it (-f %M, written to a file apart from the program's stderr),
// for the reason peakRSS gives. Then it builds h01_list_churn with its
// rounds constant set to each of heapRounds, with aether build and gc, to
// measure how the peak grows with the garbage a program makes. It writes
// heap_rss.tsv, heap_summary.tsv, heap_growth.tsv, and environment.txt.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// heapRounds are the round counts of the growth series.
var heapRounds = []int{1, 2, 4, 8, 16, 32, 64, 128}

// roundsDecl is the declaration of h01_list_churn the series replaces.
const roundsDecl = "const rounds = 64\n"

// TestBenchHeapWorkloads checks, in the normal suite, that every heap
// workload is inside the supported subset and has a recorded gc output,
// and that h01_list_churn declares rounds as the series expects.
func TestBenchHeapWorkloads(t *testing.T) {
	for _, w := range workloadsIn(t, "bench_heap") {
		if _, ds := compile([]source{{w.name + ".go", w.src}}); len(ds) > 0 {
			t.Errorf("%s: %v", w.name, ds)
		}
		if len(w.want) == 0 || w.want[len(w.want)-1] != '\n' {
			t.Errorf("%s: recorded output %q is empty or unterminated", w.name, w.want)
		}
		if w.name == "h01_list_churn" && bytes.Count(w.src, []byte(roundsDecl)) != 1 {
			t.Errorf("%s: no single %q", w.name, roundsDecl)
		}
	}
}

// heapRun runs exe under GNU time and returns its stderr, exit status, and
// peak resident set in KiB.
func heapRun(t *testing.T, exe string) ([]byte, int, int64) {
	t.Helper()
	rss := exe + ".rss"
	cmd := exec.Command("/usr/bin/time", "-f", "%M", "-o", rss, exe)
	var e bytes.Buffer
	cmd.Stderr = &e
	err := cmd.Run()
	if _, ok := err.(*exec.ExitError); err != nil && !ok {
		t.Fatalf("%s: %v", exe, err)
	}
	out, err := os.ReadFile(rss)
	if err != nil {
		t.Fatal(err)
	}
	// GNU time writes "Command exited with non-zero status" first if so.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	kib, err := strconv.ParseInt(lines[len(lines)-1], 10, 64)
	if err != nil {
		t.Fatalf("%s: GNU time wrote %q", exe, out)
	}
	return e.Bytes(), cmd.ProcessState.ExitCode(), kib
}

// heapBuild builds src as exe with variant v: "aether", "O0", or "gc".
func heapBuild(t *testing.T, aether, gobin, out, exe, v string, src []byte) {
	t.Helper()
	var c *exec.Cmd
	switch v {
	case "gc":
		mod := exe + ".mod"
		if err := os.MkdirAll(mod, 0o755); err != nil {
			t.Fatal(err)
		}
		writeReport(t, mod, "go.mod", "module bench\n\ngo 1.26\n")
		writeReport(t, mod, "main.go", string(src))
		c = exec.Command(gobin, "build", "-o", exe, ".")
		c.Dir = mod
		c.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=", "GOAMD64=v1")
	default:
		path := exe + ".go"
		writeReport(t, out, filepath.Base(path), string(src))
		args := []string{"build", "-o", exe, path}
		if v == "O0" {
			args = []string{"build", "-O0", "-o", exe, path}
		}
		c = exec.Command(aether, args...)
	}
	if o, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", exe, err, o)
	}
}

// heapMeasure runs exe runs times, after a discarded warm-up run, checks
// each run's output against want, and returns the peaks in KiB, sorted.
func heapMeasure(t *testing.T, exe string, runs int, want []byte) []int64 {
	t.Helper()
	var kibs []int64
	for r := -1; r < runs; r++ {
		stderr, code, kib := heapRun(t, exe)
		if err := checkRun(stderr, code, want); err != nil {
			t.Fatalf("%s: %v", exe, err)
		}
		if r >= 0 {
			kibs = append(kibs, kib)
		}
	}
	sort.Slice(kibs, func(i, j int) bool { return kibs[i] < kibs[j] })
	return kibs
}

func TestBenchHeap(t *testing.T) {
	out := benchOut(t)
	if _, err := os.Stat("/usr/bin/time"); err != nil {
		t.Skip("GNU time not found")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not found")
	}
	runs := envRuns(t, "AETHER_BENCH_RUNS", 7)
	aether := filepath.Join(out, "aether")
	if o, err := exec.Command(gobin, "build", "-o", aether, ".").CombinedOutput(); err != nil {
		t.Fatalf("building aether: %v\n%s", err, o)
	}
	med := func(k []int64) int64 { return k[len(k)/2] }

	var raw, sum strings.Builder
	raw.WriteString("workload\tvariant\trun\tpeak KiB\n")
	sum.WriteString("workload\tvariant\tn\tpeak median KiB\tmin-max\n")
	medians := map[[2]string]int64{}
	for _, w := range workloadsIn(t, "bench_heap") {
		for _, v := range aggVariants {
			exe := filepath.Join(out, w.name+"."+v)
			heapBuild(t, aether, gobin, out, exe, v, w.src)
			kibs := heapMeasure(t, exe, runs, w.want)
			for r, k := range kibs {
				fmt.Fprintf(&raw, "%s\t%s\t%d\t%d\n", w.name, v, r, k)
			}
			medians[[2]string{w.name, v}] = med(kibs)
			fmt.Fprintf(&sum, "%s\t%s\t%d\t%d\t%d-%d\n", w.name, v, len(kibs), med(kibs), kibs[0], kibs[len(kibs)-1])
		}
	}
	sum.WriteString("\nworkload\taether/gc peak\tO0/aether peak\n")
	for _, w := range workloadsIn(t, "bench_heap") {
		m := func(v string) float64 { return float64(medians[[2]string{w.name, v}]) }
		fmt.Fprintf(&sum, "%s\t%.2f\t%.2f\n", w.name, m("aether")/m("gc"), m("O0")/m("aether"))
	}
	writeReport(t, out, "heap_rss.tsv", raw.String())
	writeReport(t, out, "heap_summary.tsv", sum.String())

	// The growth series: gc's output for each round count is the oracle.
	var h01 benchWorkload
	for _, w := range workloadsIn(t, "bench_heap") {
		if w.name == "h01_list_churn" {
			h01 = w
		}
	}
	var growth strings.Builder
	growth.WriteString("rounds\taether median KiB\taether min-max\tgc median KiB\tgc min-max\n")
	for _, n := range heapRounds {
		src := bytes.Replace(h01.src, []byte(roundsDecl), []byte(fmt.Sprintf("const rounds = %d\n", n)), 1)
		name := fmt.Sprintf("h01_rounds%d", n)
		gcExe, aExe := filepath.Join(out, name+".gc"), filepath.Join(out, name+".aether")
		heapBuild(t, aether, gobin, out, gcExe, "gc", src)
		heapBuild(t, aether, gobin, out, aExe, "aether", src)
		want, code, _ := heapRun(t, gcExe)
		if code != 0 {
			t.Fatalf("%s: exit status %d", gcExe, code)
		}
		g, a := heapMeasure(t, gcExe, runs, want), heapMeasure(t, aExe, runs, want)
		fmt.Fprintf(&growth, "%d\t%d\t%d-%d\t%d\t%d-%d\n", n, med(a), a[0], a[len(a)-1], med(g), g[0], g[len(g)-1])
	}
	writeReport(t, out, "heap_growth.tsv", growth.String())
}
