// aether:expect run

package main

// A panic during package initialization runs the deferred calls of the
// functions it passes through, then ends the program (D-043).

var g = setup(3)

func setup(n int) int {
	defer println("setup deferred", n)
	if n > 2 {
		panic(n * 100)
	}
	return n
}

func main() {
	defer println("main deferred")
	println("unreached", g)
}
