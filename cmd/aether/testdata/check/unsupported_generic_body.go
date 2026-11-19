// aether:expect unsupported for-range

package main

// The copies of generic code are checked against the subset; a
// rejection is reported once, at the generic code's position.

func sum[T int | float64](xs []T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}

func main() {
	println(sum([]int{1}), sum([]float64{2}))
}
