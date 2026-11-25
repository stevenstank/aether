package main

import (
	"fmt"
	"slices"
)

func main() {
	s := []int{3, 1, 2}
	slices.Sort(s)
	i, ok := slices.BinarySearch(s, 2)
	s = slices.Insert(s, 0, 9)
	fmt.Println(s, i, ok, slices.Index(s, 2), slices.Contains(s, 9), slices.Equal(s, slices.Clone(s)))
	slices.SortStableFunc(s, func(a, b int) int { return b - a })
	fmt.Println(s)
}
