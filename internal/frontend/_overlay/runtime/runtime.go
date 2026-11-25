// Package runtime is Aether's replacement of gc's runtime package (D-049,
// increment 11.6): the part of its API that Aether's runtime implements.
// The functions without a body are intrinsics, but for those of extern.go.
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

// GOROOT returns the GOROOT environment variable's value, as gc's does
// when the program sets it, else "": gc's then returns the tree it was
// built with unless built with -trimpath, and an Aether program records no
// absolute path (D-054).
func GOROOT() string {
	const prefix = "GOROOT="
	for i := 0; i < envc(); i++ {
		if s := envString(i); len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return s[len(prefix):]
		}
	}
	return ""
}

// envc and envString are the program's environment strings, as execve
// passed them: intrinsics.
func envc() int
func envString(i int) string
