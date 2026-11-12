// aether:expect run

package main

// In an assignment, the operands of an index expression on the left and the
// right side are evaluated first, and the index is checked when the element
// is assigned (spec §Assignment statements): both calls print before the
// panic.

var a [3]int

func i() int {
	println("i")
	return 5
}

func f() int {
	println("f")
	return 1
}

func main() {
	a[0] = 4
	println(a[0])
	a[i()] = f()
}
