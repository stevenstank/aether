// aether:expect run

package main

// A three-index slice of an array with a maximum beyond its length.

func main() {
	var a [4]int
	m := uint16(6)
	println(len(a[1:2:4]))
	println(len(a[:2:m]))
}
