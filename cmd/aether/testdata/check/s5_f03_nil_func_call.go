// aether:expect run

package main

// A call of a nil func value panics after the function value and the
// arguments are evaluated, as gc's does (D-039).

func side(s string) int {
	println("arg", s)
	return 1
}

type T struct {
	f func(int, int) int
}

func main() {
	var t T
	println(t.f == nil)
	fs := make([]func(int, int) int, 2)
	println(len(fs), fs[1] == nil)
	t.f(side("a"), side("b"))
	println("not reached")
}
