// aether:expect run

package main

// An index out of range of a slice's length, not its capacity, panics with
// gc's message; the index of an assignment is checked after the right side.

func f() int {
	println("f")
	return 4
}

func main() {
	s := make([]int, 3, 8)
	s[2] = f()
	println(s[2], len(s))
	s[3] = f()
}
