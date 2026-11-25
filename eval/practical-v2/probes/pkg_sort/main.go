package main

import (
	"fmt"
	"sort"
)

func main() {
	s := []int{3, 1, 2}
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	f := []float64{2, 1}
	sort.Float64s(f)
	fmt.Println(s, f)
}
