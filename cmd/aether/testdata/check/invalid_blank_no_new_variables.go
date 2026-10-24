// aether:expect invalid

package main

func main() {
	a := 1
	a, _ := 2, 3
	println(a)
}
