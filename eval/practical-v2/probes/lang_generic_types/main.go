package main

import (
	"cmp"
	"fmt"
)

type Tree[K, V any] struct {
	keys []K
	vals []V
	cmp  func(a, b K) int
}

func NewTree[K, V any](c func(a, b K) int) *Tree[K, V] { return &Tree[K, V]{cmp: c} }

func (t *Tree[K, V]) Put(k K, v V) {
	for i, x := range t.keys {
		if t.cmp(x, k) == 0 {
			t.vals[i] = v
			return
		}
	}
	t.keys = append(t.keys, k)
	t.vals = append(t.vals, v)
}

func (t *Tree[K, V]) Get(k K) (V, bool) {
	for i, x := range t.keys {
		if t.cmp(x, k) == 0 {
			return t.vals[i], true
		}
	}
	var zero V
	return zero, false
}

func (t *Tree[K, V]) Len() int { return len(t.keys) }

type Set[T comparable] struct{ m map[T]bool }

func (s *Set[T]) Add(v T) bool {
	if s.m == nil {
		s.m = map[T]bool{}
	}
	if s.m[v] {
		return false
	}
	s.m[v] = true
	return true
}

func main() {
	t := NewTree[string, int](cmp.Compare[string])
	t.Put("b", 2)
	t.Put("a", 1)
	v, ok := t.Get("a")
	fmt.Println(v, ok, t.Len())
	var s Set[int]
	fmt.Println(s.Add(1), s.Add(1))
}
