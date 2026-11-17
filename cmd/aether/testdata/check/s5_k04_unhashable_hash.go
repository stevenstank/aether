// aether:expect run

package main

// The same lookup in a map with entries panics with gc's hash message
// (D-042).

func main() {
	m := map[any]int{1: 1}
	var k any = []int{1}
	println(m[k])
}
