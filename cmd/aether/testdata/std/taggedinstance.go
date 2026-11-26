package main

import (
	"fmt"
	"maps"
	"slices"
)

func first[T any](xs []T) T { return xs[0] }

func main() {
	a := map[string]struct {
		N int `json:"n"`
	}{"x": {1}, "y": {2}}
	b := map[string]struct {
		N int `json:"m"`
	}{"z": {3}}
	ka := slices.Sorted(maps.Keys(a))
	kb := slices.Sorted(maps.Keys(b))
	fmt.Println(ka, kb, first(slices.Collect(maps.Values(b))))
	fmt.Printf("%T\n%T\n", first[struct {
		N int `json:"n"`
	}], first[struct {
		N int `json:"m"`
	}])
}
