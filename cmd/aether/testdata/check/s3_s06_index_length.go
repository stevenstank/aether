// aether:expect run

package main

// An index out of range panics with gc's message, after earlier output,
// here printed without a newline.

var zero string

func main() {
	s := "abc"
	i := len(s)
	print(s, len(zero), " ")
	println(s[i])
}
