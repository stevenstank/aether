// aether:expect run

package main

// Converting a slice to an array copies its first elements; to a pointer
// to an array, it shares them (Go 1.20, v2 evaluation).

func main() {
	s := []int{1, 2, 3}
	a := [2]int(s)
	p := (*[3]int)(s)
	s[0] = 9
	p[2] = 7
	println(a[0], a[1], p[0], s[2], len([0]int(s[:0])))
}
