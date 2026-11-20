// Package abi is Aether's replacement of gc's internal/abi (D-049,
// increment 11.7), which describes gc's type metadata and calling
// convention: what strings uses of it.
package abi

import "unsafe"

// NoEscape returns p. For gc it hides p from escape analysis.
func NoEscape(p unsafe.Pointer) unsafe.Pointer {
	return p
}
