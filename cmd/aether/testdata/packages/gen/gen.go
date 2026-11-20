// Package gen has generic code that other packages instantiate (D-049,
// increment 11.7): with predeclared types, its own types, and types of
// the packages it imports.
package gen

import "example.com/pk/base"

// Number is a constraint other packages use.
type Number interface {
	~int | ~int64 | ~float64
}

// Sum adds xs up.
func Sum[T Number](xs ...T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}

// Map applies f to each element of xs.
func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

// Stack is a generic type with methods, which use an unexported generic
// helper.
type Stack[T any] struct {
	items []T
}

func (s *Stack[T]) Push(x T) { s.items = append(s.items, x) }

func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	x := last(s.items)
	s.items = s.items[:len(s.items)-1]
	return x, true
}

func (s *Stack[T]) Len() int { return len(s.items) }

func last[T any](xs []T) T { return xs[len(xs)-1] }

// Pair is instantiated by other packages with gen's own Level.
type Pair[K comparable, V any] struct {
	Key K
	Val V
}

// Level is a type of gen.
type Level int

// Levels returns pairs of gen's own type.
func Levels() []Pair[string, Level] {
	return []Pair[string, Level]{{"low", 1}, {"high", 2}}
}

// Kinds instantiates Map with a type of the package gen imports.
func Kinds(n int) []base.Kind {
	return Map([]int{n, n + 1}, func(i int) base.Kind { return base.Kind(i) })
}
