// aether:expect unsupported string-concatenation

package main

func main() {
	s := "x"
	s += "y"
	println(s + "z")
}
