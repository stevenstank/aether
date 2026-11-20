// Package iter is Aether's replacement of gc's iter package (D-049,
// increment 11.7): its iterator types. Pull and Pull2, which gc implements
// with its runtime's coroutines, are left out.
package iter

// Seq is an iterator over sequences of individual values.
type Seq[V any] func(yield func(V) bool)

// Seq2 is an iterator over sequences of pairs of values.
type Seq2[K, V any] func(yield func(K, V) bool)
