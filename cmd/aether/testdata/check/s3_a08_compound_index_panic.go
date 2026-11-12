// aether:expect run

package main

// x op= y with x an array element: the index operand and y are evaluated
// before the bounds check, as gc orders them.

type S struct {
	v [2]int8
}

func i() int {
	println("i")
	return -1
}

func f() int8 {
	println("f")
	return 1
}

func main() {
	var s S
	s.v[1] += f()
	println(s.v[1])
	s.v[i()] += f()
}
