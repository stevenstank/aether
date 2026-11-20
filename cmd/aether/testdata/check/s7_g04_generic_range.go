// aether:expect run

package main

// A range over a slice of a type parameter, in each instance (D-049).

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
