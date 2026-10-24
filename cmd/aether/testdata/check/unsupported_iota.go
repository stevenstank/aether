// aether:expect unsupported predeclared-identifier

package main

func main() {
	const c = iota
	println(c)
}
