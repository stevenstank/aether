//go:build linux && amd64
// +build linux,amd64

// aether:expect run

package main

// The directives Aether accepts and ignores (D-049): a build constraint
// before the package clause, and //go:noinline, //go:norace,
// //go:nocheckptr, and //go:registerparams before function declarations.

//go:noinline
func add(a, b int) int { return a + b }

//go:norace

// twice has a doc comment between it and its directives.
//go:nocheckptr
//go:registerparams
func twice(x int) int { return add(x, x) }

func main() {
	println(add(1, 2), twice(21))
}
