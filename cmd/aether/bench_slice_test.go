package main

// Slice benchmarks (Phase 7 increment 7.2): allocation by make, append that
// grows and append within the capacity, indexing, and overlapping copy, on
// the workloads of testdata/bench_slice, each with name.gcout.
// TestBenchSliceWorkloads checks them in the normal suite; TestBenchSlice
// measures them as TestBenchAgg does its own, when AETHER_BENCH_OUT is set,
// writing slice_compile.tsv, slice_times.tsv, slice_summary.tsv,
// slice_profile.tsv (the share of alloc, move, ldm, and stm), and
// environment.txt. No workload prints a capacity after growth (V-6).

import "testing"

func TestBenchSliceWorkloads(t *testing.T) {
	for _, w := range workloadsIn(t, "bench_slice") {
		if _, ds := compile([]source{{w.name + ".go", w.src}}); len(ds) > 0 {
			t.Errorf("%s: %v", w.name, ds)
		}
		if len(w.want) == 0 || w.want[len(w.want)-1] != '\n' {
			t.Errorf("%s: recorded output %q is empty or unterminated", w.name, w.want)
		}
	}
}

func TestBenchSlice(t *testing.T) {
	benchSuite(t, "bench_slice", "slice", []string{"alloc", "move", "ldm", "stm"})
}
