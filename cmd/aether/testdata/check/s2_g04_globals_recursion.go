// aether:expect run

package main

// Globals shared by a recursive function and its callers, and a global
// initialized from a recursive call.

var calls, depth, deepest int

var fib10 = fib(10)

func fib(n int) int {
	calls++
	depth++
	if depth > deepest {
		deepest = depth
	}
	r := n
	if n >= 2 {
		r = fib(n-1) + fib(n-2)
	}
	depth--
	return r
}

func main() {
	println(fib10, calls, depth, deepest)
	calls, deepest = 0, 0
	v := fib(20)
	println(v, calls, depth, deepest)
}
