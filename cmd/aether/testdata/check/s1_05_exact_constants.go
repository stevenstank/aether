// aether:expect run

package main

func main() {
	const big = 9223372036854775807*4 - 3*9223372036854775807
	const neg = -9223372036854775807 - 1
	x := big
	y := neg
	println(x, y)
}
