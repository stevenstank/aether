// aether:expect run

package main

// m[k]++ hashes k before reading m[k], even in an empty map (D-042).

func main() {
	m := map[any]int{}
	var k any = map[int]int{}
	m[k]++
}
