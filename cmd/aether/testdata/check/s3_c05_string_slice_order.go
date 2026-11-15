// aether:expect run

package main

// A low bound beyond the high bound panics, after both bounds are evaluated.

func at(i int) int {
	println("bound", i)
	return i
}

func main() {
	s := "abcdef"
	println(s[at(1):at(4)])
	println(s[at(4):at(2)])
}
