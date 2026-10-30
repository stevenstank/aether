// aether:expect run

package main

// A panic during package initialization ends the program before main
// runs, after the output already written.

var zero int

var x = 7

var y = show(x) / zero

func show(n int) int {
	print(n)
	return n
}

func main() {
	println(y)
}
