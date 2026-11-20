// aether:expect unsupported import
// aether:gc invalid

package main

import "example.com/lib"

func main() {
	var x int = "s"
	println(x)
}
