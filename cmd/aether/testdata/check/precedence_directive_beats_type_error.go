// aether:expect unsupported directive
// aether:gc invalid

package main

//go:nosplit
func main() {
	var x int = "s"
	println(x)
}
