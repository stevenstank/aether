package main

import (
	"fmt"
	"maps"
	"slices"
)

func main() {
	m := map[string]int{"b": 2, "a": 1}
	fmt.Println(slices.Sorted(maps.Keys(m)), len(maps.Clone(m)))
}
