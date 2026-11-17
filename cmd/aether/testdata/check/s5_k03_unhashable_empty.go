// aether:expect run

package main

// A key whose dynamic type is not comparable, looked up in an empty map,
// panics with gc's key-check message (D-042).

func main() {
	m := map[any]int{}
	var k any = []int{1}
	println(len(m))
	println(m[k])
}
