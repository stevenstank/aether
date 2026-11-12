// aether:expect run

package main

// Reading an array element out of range panics with gc's message, after
// earlier output.

func main() {
	var a [4]int16
	a[3] = 7
	k := uint8(4)
	println(a[3], len(a))
	println(a[k])
}
