// Package reflectlite is Aether's replacement of gc's internal/reflectlite
// (D-049, increment 11.7): the declarations sort uses, without bodies, so
// that sort.Slice and the like are unsupported where they are reached
// until Aether has reflection.
package reflectlite

// Value is the reflection interface to a Go value.
type Value struct{}

// ValueOf returns a Value for i.
func ValueOf(i any) Value

// Len returns v's length.
func (v Value) Len() int

// Swapper returns a function that swaps the elements in a slice.
func Swapper(slice any) func(i, j int)
