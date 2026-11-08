// aether:expect unsupported builtin

package main

func main() {
	x := 1.5
	println(real(complex(x, 2)))
}
