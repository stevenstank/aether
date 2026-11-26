package main

import "fmt"

type T struct{ x int }

var kept []*T

func (t *T) m() int { return t.x }
func (t *T) keep()  { kept = append(kept, t) }

func main() {
	var slices [][]int
	for a := [2]int{}; a[0] < 3; a[0]++ {
		slices = append(slices, a[:])
	}
	fmt.Println(slices)

	var elems []*int
	for a := [2]int{}; a[0] < 3; a[0]++ {
		a[1] = a[0] * 10
		elems = append(elems, &a[1])
	}
	for _, p := range elems {
		fmt.Print(*p, " ")
	}
	fmt.Println()

	var ms []func() int
	for t := (T{}); t.x < 3; t.x++ {
		ms = append(ms, t.m)
	}
	for _, f := range ms {
		fmt.Print(f(), " ")
	}
	fmt.Println()

	for t := (T{}); t.x < 3; t.x++ {
		t.keep()
	}
	for _, t := range kept {
		fmt.Print(t.x, " ")
	}
	fmt.Println()

	var fields []*int
	for s := (struct{ a, b int }{}); s.a < 3; s.a++ {
		fields = append(fields, &s.b)
		s.b = s.a + 100
	}
	for _, p := range fields {
		fmt.Print(*p, " ")
	}
	fmt.Println()
}
