// aether:expect run

package main

// Slicing through a nil pointer to an array panics.

func main() {
	var p *[3]int
	println(len(p), cap(p))
	println(len(p[:1]))
}
