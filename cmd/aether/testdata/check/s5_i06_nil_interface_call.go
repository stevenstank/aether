// aether:expect run

package main

// A method call on a nil interface panics after its arguments are
// evaluated, as gc's does (D-041).

type I interface{ M(int) int }

func arg() int {
	println("arg")
	return 1
}

func main() {
	var i I
	println(i == nil)
	println(i.M(arg()))
}
