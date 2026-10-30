// aether:expect run

package main

// print leaves no newline, so gc's panic message follows on the same line.
func main() {
	var x, y uint8 = 7, 0
	print(5)
	println(x % y)
}
