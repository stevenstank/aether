package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// baselineDir holds the register-allocation baseline of 2026-11-05, and
// ra0Dir the RA-0 results of 2026-11-06 (D-026).
var (
	baselineDir = filepath.Join("..", "..", "docs", "benchmarks", "2026-11-05-regalloc", "data")
	ra0Dir      = filepath.Join("..", "..", "docs", "benchmarks", "2026-11-06-ra0", "data")
)

// TestBenchBaselineData checks the recorded baseline against the checked-in
// harness and workloads: the timing summaries the report quotes are
// timesSummary of the raw times, and the deterministic reports cover
// exactly the workloads of testdata/bench.
func TestBenchBaselineData(t *testing.T) {
	sessions := [][2]string{{baselineDir, "times_session1"}, {baselineDir, "times_session2"},
		{baselineDir, "times_session3"}, {ra0Dir, "times"}}
	for _, ds := range sessions {
		dir, session := ds[0], ds[1]
		raw, err := os.ReadFile(filepath.Join(dir, session+".tsv"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(dir, session+"_summary.tsv"))
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
