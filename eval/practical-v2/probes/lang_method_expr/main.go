package main

import (
	"fmt"
	"slices"
)

type V struct{ N int }

func (v V) Compare(o V) int { return v.N - o.N }
func (v *V) Bump()          { v.N += 10 }

func main() {
	vs := []V{{3}, {1}, {2}}
	slices.SortStableFunc(vs, V.Compare)
	f := (*V).Bump
	f(&vs[0])
	fmt.Println(vs)
}
