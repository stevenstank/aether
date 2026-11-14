// aether:expect unsupported local-type-declaration

package main

func main() {
	type P *int
	x := 1
	var p P = &x
	println(*p)
}
