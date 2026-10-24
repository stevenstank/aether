// aether:expect unsupported assignment-target

package main

func main() {
	x := 1
	(x) = 2
	(x)++
	println(x)
}
