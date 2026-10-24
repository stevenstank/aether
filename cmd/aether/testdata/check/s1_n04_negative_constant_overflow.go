// aether:expect invalid

package main

func main() {
	const c = -9223372036854775807 - 2
	x := c
	println(x)
}
