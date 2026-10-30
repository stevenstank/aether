// aether:expect run

package main

// Package initialization follows types.Info.InitOrder (spec §Package
// initialization): a variable is initialized after the variables its
// initializer refers to, directly or through the functions it calls, and
// otherwise in declaration order. Each initializer prints its number when it
// runs: trace prints n, pair 4, and readB 5.

var a = trace(1, c+1)

var b = trace(2, 10)

var c = trace(3, b*2)

var d, e = pair()

var f = g()

var unset int

var h = trace(6, d+e+f)

func trace(n, v int) int {
	println(n, v)
	return v
}

func pair() (int, int) {
	println(4, a)
	return a, -a
}

// g depends on b only through another function.
func g() int {
	return readB() + 100
}

func readB() int {
	println(5, b)
	return b
}

func main() {
	println(a, b, c, d, e, f, unset, h)
}
