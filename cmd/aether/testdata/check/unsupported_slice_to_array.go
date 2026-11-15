// aether:expect unsupported conversion

package main

// Converting a slice to an array is not supported (D-036).

func main() {
	s := []int{1, 2}
	a := [2]int(s)
	println(a[1])
}
