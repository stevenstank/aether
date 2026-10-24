// aether:expect run

package main

func main() {
	var x int
	x += 5
	x -= 7
	x *= -3
	x++
	x--
	x--
	println(x)
	var p, q int = 10, -10
	var r = p * q
	println(p, q, r)
}
