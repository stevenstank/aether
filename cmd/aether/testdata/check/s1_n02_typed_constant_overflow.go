// aether:expect invalid

package main

func main() {
	const c int = 9223372036854775807
	x := c + 1
	println(x)
}
