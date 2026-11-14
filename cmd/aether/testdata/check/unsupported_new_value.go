// aether:expect unsupported builtin

package main

// new of a value, rather than of a type, which Go 1.26 added.

func main() {
	p := new(42)
	println(*p)
}
