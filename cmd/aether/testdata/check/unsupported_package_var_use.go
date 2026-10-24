// aether:expect unsupported package-level-declaration

package main

var g int

const k = 2

func main() {
	g = 1
	g++
	x := g + k
	println(x)
}
