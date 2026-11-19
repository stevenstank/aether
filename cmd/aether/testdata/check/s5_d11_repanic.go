// aether:expect run

package main

// A recovered value panicked again is written once, " [recovered,
// repanicked]"; a run-time error's message is its Error (D-043).

func get(s []int, i int) int {
	defer func() { panic(recover()) }()
	return s[i]
}

func main() {
	defer println("main deferred")
	println(get([]int{1}, 3))
}
