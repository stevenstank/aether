package main

// Summary statistics of the benchmark timings (BENCHMARKS.md: the median
// and the spread, never a single run). The quartiles are those of Python's
// statistics.quantiles(n=4) with its default "exclusive" method, which
// summarized the 2026-11-05 baseline before this harness was checked in;
// keeping the method keeps later summaries comparable with it.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// benchVariants are the executables of each workload, in timing order.
var benchVariants = []string{"slot", "ssa", "ra", "isel", "abir1", "two", "fold", "dce", "copy", "gc", "gcN"}

// median returns the median of the sorted xs.
func median(xs []float64) float64 {
	n := len(xs)
	if n%2 == 1 {
		return xs[n/2]
	}
	return (xs[n/2-1] + xs[n/2]) / 2
}

// quartiles returns the first and third quartiles of the sorted xs, at
// least two values, by the exclusive method: the rank of quartile i of n
// values is i(n+1)/4, interpolated linearly and clamped to the data.
func quartiles(xs []float64) (q1, q3 float64) {
	n := len(xs)
	m := n + 1
	q := func(i int) float64 {
		j := min(max(i*m/4, 1), n-1)
		delta := i*m - j*4
		return (xs[j-1]*float64(4-delta) + xs[j]*float64(delta)) / 4
	}
	return q(1), q(3)
}

// timeStats summarizes the runs of one executable, in milliseconds.
type timeStats struct {
	n                                    int
	cpuMed, cpuQ1, cpuQ3, cpuMin, cpuMax float64
	wallMed, wallMin, wallMax            float64
}

func summarize(cpu, wall []float64) timeStats {
	slices.Sort(cpu)
	slices.Sort(wall)
	s := timeStats{n: len(cpu), cpuMed: median(cpu), cpuMin: cpu[0], cpuMax: cpu[len(cpu)-1],
		wallMed: median(wall), wallMin: wall[0], wallMax: wall[len(wall)-1]}
	s.cpuQ1, s.cpuQ3 = quartiles(cpu)
	return s
}

// timesSummary summarizes times.tsv: per workload and variant, the CPU time
// median, interquartile range, and range, and the wall time median and
// range; then, per workload, the ratios of the CPU medians and whether the
// slot and SSA ranges overlap; and, if there are RA-0 times, the same for
// RA-0 against SSA.
func timesSummary(raw string) (string, error) {
	type key struct{ w, v string }
	cpu, wall := map[key][]float64{}, map[key][]float64{}
	var workloads []string
	for i, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		if i == 0 {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			return "", fmt.Errorf("times line %d: %q", i+1, line)
		}
		w, err1 := strconv.ParseInt(f[3], 10, 64)
		c, err2 := strconv.ParseInt(f[4], 10, 64)
		if err1 != nil || err2 != nil {
			return "", fmt.Errorf("times line %d: %q", i+1, line)
		}
		k := key{f[0], f[1]}
		if !slices.Contains(workloads, f[0]) {
			workloads = append(workloads, f[0])
		}
		wall[k] = append(wall[k], float64(w)/1e6)
		cpu[k] = append(cpu[k], float64(c)/1e6)
	}
	slices.Sort(workloads)
	stats := map[key]timeStats{}
	var b strings.Builder
	b.WriteString("workload\tvariant\tn\tcpu median ms\tcpu IQR\tcpu min-max\twall median ms\twall min-max\n")
	for _, w := range workloads {
		for _, v := range benchVariants {
			k := key{w, v}
			if len(cpu[k]) < 2 {
				continue
			}
			s := summarize(cpu[k], wall[k])
			stats[k] = s
			fmt.Fprintf(&b, "%s\t%s\t%d\t%.1f\t%.1f-%.1f\t%.1f-%.1f\t%.1f\t%.1f-%.1f\n", w, v, s.n,
				s.cpuMed, s.cpuQ1, s.cpuQ3, s.cpuMin, s.cpuMax, s.wallMed, s.wallMin, s.wallMax)
		}
	}
	b.WriteString("\nworkload\tssa/slot cpu\tranges overlap?\tslot/gc\tssa/gc\tssa/gcN\n")
	for _, w := range workloads {
		s, okS := stats[key{w, "slot"}]
		a, okA := stats[key{w, "ssa"}]
		if !okS || !okA {
			continue
		}
		overlap := "yes"
		if a.cpuMax < s.cpuMin || s.cpuMax < a.cpuMin {
			overlap = "no"
		}
		ratio := func(x timeStats, v string) string {
			if g, ok := stats[key{w, v}]; ok {
				return fmt.Sprintf("%.2f", x.cpuMed/g.cpuMed)
			}
			return "n/a"
		}
		fmt.Fprintf(&b, "%s\t%.3f\t%s\t%s\t%s\t%s\n", w, a.cpuMed/s.cpuMed, overlap, ratio(s, "gc"), ratio(a, "gc"), ratio(a, "gcN"))
	}
	header := false
	for _, w := range workloads {
		a, okA := stats[key{w, "ssa"}]
		r, okR := stats[key{w, "ra"}]
		if !okA || !okR {
			continue
		}
		if !header {
			b.WriteString("\nworkload\tra/ssa cpu\tranges overlap?\tra/slot\tra/gc\tra/gcN\n")
			header = true
		}
		overlap := "yes"
		if r.cpuMax < a.cpuMin || a.cpuMax < r.cpuMin {
			overlap = "no"
		}
		ratio := func(v string) string {
			if g, ok := stats[key{w, v}]; ok {
				return fmt.Sprintf("%.2f", r.cpuMed/g.cpuMed)
			}
			return "n/a"
		}
		fmt.Fprintf(&b, "%s\t%.3f\t%s\t%s\t%s\t%s\n", w, r.cpuMed/a.cpuMed, overlap, ratio("slot"), ratio("gc"), ratio("gcN"))
	}
	header = false
	for _, w := range workloads {
		r, okR := stats[key{w, "ra"}]
		i, okI := stats[key{w, "isel"}]
		if !okR || !okI {
			continue
		}
		if !header {
			b.WriteString("\nworkload\tisel/ra cpu\tranges overlap?\tisel/gc\tisel/gcN\tra/gc\n")
			header = true
		}
		overlap := "yes"
		if i.cpuMax < r.cpuMin || r.cpuMax < i.cpuMin {
			overlap = "no"
		}
		ratio := func(x timeStats, v string) string {
			if g, ok := stats[key{w, v}]; ok {
				return fmt.Sprintf("%.2f", x.cpuMed/g.cpuMed)
			}
			return "n/a"
		}
		fmt.Fprintf(&b, "%s\t%.3f\t%s\t%s\t%s\t%s\n", w, i.cpuMed/r.cpuMed, overlap, ratio(i, "gc"), ratio(i, "gcN"), ratio(r, "gc"))
	}
	return b.String(), nil
}
