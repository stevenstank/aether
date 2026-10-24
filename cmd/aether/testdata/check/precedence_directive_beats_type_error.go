// aether:expect unsupported directive
// aether:gc invalid

package main

//go:noinline
func main() {
	var x int = "s"
	println(x)
}
