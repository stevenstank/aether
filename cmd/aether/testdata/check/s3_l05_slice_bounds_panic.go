// aether:expect run

package main

// A slice expression's high bound may reach the capacity, but a low bound
// above the high one panics with gc's message.

func main() {
	s := make([]int, 3, 5)
	i, j := 4, 5
	println(len(s[:j]), cap(s[i:j]))
	println(len(s[i:]))
}
