package main

import (
	"strings"
	"testing"
)

// TestBenchQuartiles checks median and quartiles against Python's
// statistics.median and statistics.quantiles(n=4) on the same data
// (Python 3.12.3).
func TestBenchQuartiles(t *testing.T) {
	for _, c := range []struct {
		xs          []float64
		med, q1, q3 float64
	}{
		{[]float64{1, 2}, 1.5, 0.75, 2.25},
		{[]float64{1, 2, 3}, 2, 1, 3},
		{[]float64{1, 2, 3, 4}, 2.5, 1.25, 3.75},
		{[]float64{1, 2, 3, 4, 5, 7, 9}, 4, 2, 7},
		{[]float64{1.25, 2, 3, 4, 5, 6, 7.75, 8, 9, 10.5, 11, 12, 13, 14, 15.5}, 8, 4, 12},
	} {
		q1, q3 := quartiles(c.xs)
		if m := median(c.xs); m != c.med || q1 != c.q1 || q3 != c.q3 {
			t.Errorf("%v: median %v, quartiles %v %v; want %v, %v %v", c.xs, m, q1, q3, c.med, c.q1, c.q3)
		}
	}
}

func TestBenchTimesSummary(t *testing.T) {
	raw := "workload\tvariant\trun\twall ns\tcpu ns\n" +
		"w\tslot\t0\t3000000\t2000000\nw\tssa\t0\t2000000\t1000000\n" +
		"w\tslot\t1\t5000000\t4000000\nw\tssa\t1\t2000000\t1500000\n"
	got, err := timesSummary(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "workload\tvariant\tn\tcpu median ms\tcpu IQR\tcpu min-max\twall median ms\twall min-max\n" +
		"w\tslot\t2\t3.0\t1.5-4.5\t2.0-4.0\t4.0\t3.0-5.0\n" +
		"w\tssa\t2\t1.2\t0.9-1.6\t1.0-1.5\t2.0\t2.0-2.0\n" +
		"\nworkload\tssa/slot cpu\tranges overlap?\tslot/gc\tssa/gc\tssa/gcN\n" +
		"w\t0.417\tno\tn/a\tn/a\tn/a\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	got, err = timesSummary(raw + "w\tra\t0\t1000000\t500000\nw\tra\t1\t1000000\t700000\n")
	if err != nil {
		t.Fatal(err)
	}
	wantRA := "w\tra\t2\t0.6\t0.5-0.8\t0.5-0.7\t1.0\t1.0-1.0\n"
	want = strings.Replace(want, "\nworkload\tssa/slot", wantRA+"\nworkload\tssa/slot", 1) +
		"\nworkload\tra/ssa cpu\tranges overlap?\tra/slot\tra/gc\tra/gcN\n" +
		"w\t0.480\tno\t0.20\tn/a\tn/a\n"
	if got != want {
		t.Errorf("with RA-0 times: got\n%s\nwant\n%s", got, want)
	}
	if _, err := timesSummary(strings.Replace(raw, "2000000\t1000000", "x\t1000000", 1)); err == nil {
		t.Error("malformed times accepted")
	}
}
