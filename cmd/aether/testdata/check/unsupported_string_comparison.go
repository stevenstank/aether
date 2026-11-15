// aether:expect unsupported string-comparison

package main

// Strings compare for equality (D-037) but are not ordered.

func main() {
	s := "x"
	println(s == "x", s < "y", s >= s)
}
