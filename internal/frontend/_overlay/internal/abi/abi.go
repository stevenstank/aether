// Package abi is Aether's replacement of gc's internal/abi (D-049,
// increment 11.7), which describes gc's type metadata and calling
// convention: what strings uses of it.
package abi

import "unsafe"

// NoEscape returns p. For gc it hides p from escape analysis.
func NoEscape(p unsafe.Pointer) unsafe.Pointer {
	return p
}

// Type is gc's type descriptor, which Aether's are not: opaque, for the
// signatures of internal/race, whose functions do nothing (D-056).
type Type struct{}

// Escape returns x. For gc it makes x escape to the heap; Aether's
// escape analysis keeps whatever a pointer is taken of on the heap.
func Escape[T any](x T) T { return x }
