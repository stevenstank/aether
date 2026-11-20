// aether:expect unsupported import
// aether:gc invalid

package main

import "runtime"

func main() {
	var x int = "s"
	println(x)
}
