package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stevenstank/aether/internal/amd64"
	"github.com/stevenstank/aether/internal/toolchain"
)

// latencyPrograms are the workloads and the three largest S2 corpus programs.
func latencyPrograms(t *testing.T) []s2Program {
	progs := []s2Program{}
	for _, w := range benchWorkloads(t) {
		progs = append(progs, s2Program{w.name + ".go", w.src})
	}
	corpus, _ := filepath.Glob(filepath.Join("testdata", "check", "s2_*.go"))
	sort.Slice(corpus, func(i, j int) bool {
		a, _ := os.Stat(corpus[i])
		b, _ := os.Stat(corpus[j])
		return a.Size() > b.Size() || a.Size() == b.Size() && corpus[i] < corpus[j]
	})
	for _, path := range corpus[:3] {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs = append(progs, s2Program{filepath.Base(path), src})
	}
	return progs
}

// TestBenchLatency times the stages of aether build in process, with the
// compiler's own functions, on the workloads and the three largest S2 corpus
// programs: the frontend and lowering (compile), promotion, the passes, and
// emission for the default build and for -O0, and assemble, which finds and
// identifies as and ld, writes the files, and runs them; identify is
// assemble's first part alone, timed separately. It writes every run
// to latency.tsv and the medians to latency_summary.tsv. It runs only when
// AETHER_BENCH_OUT is set, AETHER_BENCH_RUNS times after a discarded warm-up.
func TestBenchLatency(t *testing.T) {
	dir := benchOut(t)
	runs, err := strconv.Atoi(os.Getenv("AETHER_BENCH_RUNS"))
	if err != nil || runs < 2 {
		t.Fatalf("AETHER_BENCH_RUNS=%q: want an integer of at least 2", os.Getenv("AETHER_BENCH_RUNS"))
	}
	progs := latencyPrograms(t)
	stages := []string{"compile", "optimize+emit", "emit -O0", "assemble", "identify"}
	exe := filepath.Join(t.TempDir(), "out")
	var raw strings.Builder
	raw.WriteString("program\tstage\trun\tns\n")
	times := map[[2]string][]float64{}
	for _, p := range progs {
		for r := -1; r < runs; r++ {
			var d [5]time.Duration
			start := time.Now()
			prog, ds := compile([]source{{p.name, p.src}})
			d[0] = time.Since(start)
			if len(ds) > 0 {
				t.Fatalf("%s: %v", p.name, ds)
			}
			start = time.Now()
			asm, err := emitProgram(prog)
			d[1] = time.Since(start)
			if err != nil {
				t.Fatalf("%s: %v", p.name, err)
			}
			slot, _ := compile([]source{{p.name, p.src}})
			start = time.Now()
			if _, err := amd64.Emit(slot); err != nil {
				t.Fatalf("%s: %v", p.name, err)
			}
			d[2] = time.Since(start)
			start = time.Now()
			if ds, err := assemble(asm, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
				t.Fatalf("%s: %v %v", p.name, ds, err)
			}
			d[3] = time.Since(start)
			start = time.Now()
			for _, tool := range []string{"as", "ld"} {
				if _, err := toolchain.Find(tool, nil); err != nil {
					t.Fatal(err)
				}
			}
			d[4] = time.Since(start)
			if r < 0 {
				continue
			}
			for i, s := range stages {
				fmt.Fprintf(&raw, "%s\t%s\t%d\t%d\n", p.name, s, r, d[i].Nanoseconds())
				k := [2]string{p.name, s}
				times[k] = append(times[k], float64(d[i].Nanoseconds())/1e6)
			}
		}
	}
	writeReport(t, dir, "latency.tsv", raw.String())
	var sum strings.Builder
	sum.WriteString("program\tcompile ms\toptimize+emit ms\temit -O0 ms\tassemble ms\tof which identify ms\tassemble share of default build\n")
	for _, p := range progs {
		var med [5]float64
		for i, s := range stages {
			x := times[[2]string{p.name, s}]
			slices.Sort(x)
			med[i] = median(x)
		}
		fmt.Fprintf(&sum, "%s\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%.2f\n", p.name, med[0], med[1], med[2], med[3], med[4], med[3]/(med[0]+med[1]+med[3]))
	}
	writeReport(t, dir, "latency_summary.tsv", sum.String())
}
