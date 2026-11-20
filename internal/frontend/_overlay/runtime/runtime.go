// Package runtime is Aether's replacement of gc's runtime package (D-049,
// increment 11.6): the part of its API that Aether's runtime implements.
// The functions without a body are intrinsics.
package runtime

// GOOS and GOARCH are the target's operating system and architecture.
const GOOS string = "linux"
const GOARCH string = "amd64"

// GC runs a garbage collection.
func GC()

// Gosched yields to the other runnable goroutines, if there are any.
func Gosched()

// NumGoroutine returns the number of goroutines that exist.
func NumGoroutine() int
