package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Set is an unordered set of comparable values.
type Set[T comparable] map[T]struct{}

func SetOf[T comparable](xs ...T) Set[T] {
	s := make(Set[T], len(xs))
	for _, x := range xs {
		s.Add(x)
	}
	return s
}

func (s Set[T]) Add(x T)      { s[x] = struct{}{} }
func (s Set[T]) Has(x T) bool { _, ok := s[x]; return ok }
func (s Set[T]) Remove(x T)   { delete(s, x) }
func (s Set[T]) Len() int     { return len(s) }

func (s Set[T]) Union(o Set[T]) Set[T] {
	r := make(Set[T], len(s)+len(o))
	for x := range s {
		r.Add(x)
	}
	for x := range o {
		r.Add(x)
	}
	return r
}

func (s Set[T]) Intersect(o Set[T]) Set[T] {
	r := Set[T]{}
	for x := range s {
		if o.Has(x) {
			r.Add(x)
		}
	}
	return r
}

func (s Set[T]) Diff(o Set[T]) Set[T] {
	r := Set[T]{}
	for x := range s {
		if !o.Has(x) {
			r.Add(x)
		}
	}
	return r
}

// Sorted returns the elements in order; T must be ordered.
func Sorted[T cmp.Ordered](s Set[T]) []T {
	out := make([]T, 0, len(s))
	for x := range s {
		out = append(out, x)
	}
	slices.Sort(out)
	return out
}

func FormatSet[T cmp.Ordered](s Set[T]) string {
	parts := []string{}
	for _, x := range Sorted(s) {
		parts = append(parts, fmt.Sprint(x))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// Map applies f to every element of xs.
func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

// Filter keeps the elements of xs for which keep is true.
func Filter[T any](xs []T, keep func(T) bool) []T {
	var out []T
	for _, x := range xs {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

// Reduce folds xs from the left.
func Reduce[T, A any](xs []T, init A, f func(A, T) A) A {
	acc := init
	for _, x := range xs {
		acc = f(acc, x)
	}
	return acc
}
