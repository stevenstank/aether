// aether:expect run

package main

// x op= y through a nil pointer evaluates y, then panics.

func g() int {
	println("g")
	return 2
}

func main() {
	n := new(int)
	*n += g()
	println(*n)
	var p *int
	*p += g()
	println("unreachable")
}
