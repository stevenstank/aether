// aether:expect unsupported builtin

package main

func main() {
	x := cap([3]int{})
	println(x)
}
