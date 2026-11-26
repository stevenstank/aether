// Package unique is Aether's replacement of gc's unique (v2 evaluation).
// gc's canonicalizes values in a map of weak pointers, which its collector
// clears; Aether's collector has no weak pointers, so its map holds the
// canonical copies strongly. Handles keep gc's guarantee: two are equal if
// and only if the values used to make them are equal. A canonical value is
// never reclaimed, where gc's may be once no handle refers to it.
package unique

import "sync"

// Handle is a globally unique identity for some value of type T.
type Handle[T comparable] struct {
	value *T
}

// Value returns a shallow copy of the T value that produced the Handle.
// Value is safe for concurrent use by multiple goroutines.
func (h Handle[T]) Value() T {
	return *h.value
}

var (
	mu        sync.Mutex
	canonical map[any]any // each value, of any type, to its *T
)

// Make returns a globally unique handle for a value of type T. Handles
// are equal if and only if the values used to produce them are equal.
// Make is safe for concurrent use by multiple goroutines.
func Make[T comparable](value T) Handle[T] {
	mu.Lock()
	defer mu.Unlock()
	if p, ok := canonical[value]; ok {
		return Handle[T]{p.(*T)}
	}
	if canonical == nil {
		canonical = map[any]any{}
	}
	p := new(T)
	*p = value
	canonical[value] = p
	return Handle[T]{p}
}
