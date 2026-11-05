package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// baselineDir holds the register-allocation baseline of 2026-11-05.
var baselineDir = filepath.Join("..", "..", "docs", "benchmarks", "2026-11-05-regalloc", "data")

// TestBenchBaselineData checks the recorded baseline against the checked-in
// harness and workloads: the timing summaries the report quotes are
// timesSummary of the raw times, and the deterministic reports cover
// exactly the workloads of testdata/bench.
func TestBenchBaselineData(t *testing.T) {
	for _, session := range []string{"times_session1", "times_session2", "times_session3"} {
		raw, err := os.ReadFile(filepath.Join(baselineDir, session+".tsv"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(baselineDir, session+"_summary.tsv"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := timesSummary(string(raw))
		if err != nil {
			t.Fatalf("%s: %v", session, err)
		}
		if got != string(want) {
			t.Errorf("%s: timesSummary differs from the recorded summary:\n%s", session, got)
		}
	}
	var names []string
	for _, w := range benchWorkloads(t) {
		names = append(names, w.name)
	}
	for _, file := range []string{"summary.tsv", "model.tsv", "times_session1.tsv"} {
		data, err := os.ReadFile(filepath.Join(baselineDir, file))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n")[1:] {
			if w, _, _ := strings.Cut(line, "\t"); !slices.Contains(got, w) {
				got = append(got, w)
			}
		}
		if !slices.Equal(got, names) {
			t.Errorf("%s covers workloads %v, want %v", file, got, names)
		}
	}
}
