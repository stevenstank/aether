package main

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"sort"
)

func printInts(xs []int) {
	for _, x := range xs {
		print(x, " ")
	}
	println()
}

// largest instantiates another package's generic code with its own type
// parameter.
func largest[T cmp.Ordered](xs []T) T {
	s := slices.Clone(xs)
	slices.Sort(s)
	return s[len(s)-1]
}

func byLen(a, b string) int { return cmp.Compare(len(a), len(b)) }

func main() {
	// slices
	x := []int{5, 2, 8, 1, 9, 2}
	println(slices.Contains(x, 8), slices.Index(x, 2), slices.Index(x, 7), slices.Max(x), slices.Min(x))
	slices.Sort(x)
	printInts(x)
	i, found := slices.BinarySearch(x, 8)
	println(i, found, slices.IsSorted(x))
	x = slices.Compact(x)
	printInts(x)
	slices.Reverse(x)
	printInts(x)
	println(slices.Equal(x, []int{9, 8, 5, 2, 1}), slices.Compare([]int{1, 2}, []int{1, 3}), slices.IndexFunc(x, func(v int) bool { return v < 3 }))
	words := []string{"ccc", "a", "bb", "dd", "e"}
	slices.SortStableFunc(words, byLen)
	println(words[0], words[1], words[2], words[3], words[4])
	slices.SortFunc(words, func(a, b string) int { return cmp.Compare(b, a) })
	println(words[0], words[4], slices.ContainsFunc(words, func(s string) bool { return len(s) == 3 }))
	big := make([]int, 300)
	for k := range big {
		big[k] = (k * 7919) % 300
	}
	slices.Sort(big)
	println(big[0], big[150], big[299], slices.IsSorted(big))
	fs := []float64{2.5, math.NaN(), -1, 0}
	slices.Sort(fs)
	println(math.IsNaN(fs[0]), fs[1], fs[3])
	println(largest([]string{"x", "z", "y"}), largest([]float64{1.5, -2}))

	// clear of a slice zeroes the elements in its length.
	type pt struct {
		x int
		s string
	}
	cl := []pt{{1, "a"}, {2, "b"}, {3, "c"}}
	clear(cl[1:2])
	println(cl[0].x, cl[1].x, cl[1].s == "", cl[2].s, len(cl))
	var nilSlice []int
	clear(nilSlice)

	// cmp
	println(cmp.Compare(1, 2), cmp.Compare("b", "a"), cmp.Less(math.NaN(), 1.0), cmp.Compare(math.NaN(), math.NaN()), cmp.Or(0, 0, 3, 4), cmp.Or("", "z"))

	// maps
	m := map[string]int{"a": 1, "b": 2}
	m2 := maps.Clone(m)
	m2["c"] = 3
	println(len(m), len(m2), maps.Equal(m, m2))
	maps.DeleteFunc(m2, func(k string, v int) bool { return v > 2 })
	println(maps.Equal(m, m2), maps.EqualFunc(m, map[string]float64{"a": 1, "b": 2}, func(a int, b float64) bool { return float64(a) == b }))
	maps.Copy(m2, map[string]int{"z": 26})
	println(len(m2), m2["z"])
	var nilMap map[int]bool
	println(maps.Clone(nilMap) == nil)

	// sort
	a := []int{3, 1, 2}
	sort.Ints(a)
	printInts(a)
	println(sort.SearchInts(a, 2), sort.IntsAreSorted(a), sort.Search(100, func(i int) bool { return i*i >= 50 }))
	s := []string{"b", "c", "a"}
	sort.Strings(s)
	println(s[0], s[1], s[2], sort.SearchStrings(s, "bb"))
	f := []float64{2, math.Inf(-1), 1}
	sort.Float64s(f)
	println(f[0], f[2])
	sort.Sort(sort.IntSlice(a))
	printInts(a)
	sort.Stable(sort.StringSlice(s))
	println(s[0], sort.IsSorted(sort.IntSlice(a)))
}
