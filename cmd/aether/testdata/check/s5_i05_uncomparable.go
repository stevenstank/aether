// aether:expect run

package main

// Comparing interfaces holding values of the same uncomparable type panics
// with gc's message; of different types, it is false (D-041).

func main() {
	var x, y any = []int{1}, true
	println(x == y)
	y = struct{ f func() }{}
	println(x == y)
	y = []int{1}
	println("before")
	println(x == y)
}
