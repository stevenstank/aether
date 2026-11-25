// Package shape instantiates another package's generic code with its own
// types, exported and not, so it hosts those instances (D-052).
package shape

import "example.com/pk/gen"

// Point is a type of shape's that main also uses in gen.Stack.
type Point struct{ X, Y int }

type tag string

// Stack returns a stack of ps: an instance shape hosts, which main uses.
func Stack(ps ...Point) *gen.Stack[Point] {
	s := &gen.Stack[Point]{}
	for _, p := range ps {
		s.Push(p)
	}
	return s
}

// Tags maps ns through shape's unexported type tag.
func Tags(ns []int) []string {
	ts := gen.Map(ns, func(n int) tag { return tag(rune('a' + n)) })
	return gen.Map(ts, func(t tag) string { return string(t) + "!" })
}
