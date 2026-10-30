// aether:expect run

package main

func div(a, b int) int {
	return a / b
}

// The arguments of a println are evaluated before it prints, so the
// panic in the second suppresses the first.
func main() {
	println(div(9, 3))
	println(div(8, 2), div(1, 0))
}
