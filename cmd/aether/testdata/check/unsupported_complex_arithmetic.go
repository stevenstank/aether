// aether:expect unsupported complex-arithmetic

package main

func main() {
	x := 1.5
	c := complex(x, 2)
	println(real(c * c))
}
