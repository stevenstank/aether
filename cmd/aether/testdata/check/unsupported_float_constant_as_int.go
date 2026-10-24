// aether:expect unsupported non-int-type

package main

func main() {
	const c = 2.5 * 2
	var y int = c
	println(y)
}
