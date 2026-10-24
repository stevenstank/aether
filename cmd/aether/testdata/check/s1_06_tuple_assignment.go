// aether:expect run

package main

func main() {
	a, b := 1, 2
	a, b = b, a
	println(a, b)
	a, b = a+b, a*b
	println(a, b)
}
