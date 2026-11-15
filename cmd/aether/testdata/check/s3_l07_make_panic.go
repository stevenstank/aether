// aether:expect run

package main

// make with a length above the capacity panics with gc's message.

func main() {
	n, c := 2, 3
	println(len(make([]int, n, c)))
	n = 4
	println(len(make([]int, n, c)))
}
