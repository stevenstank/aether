// aether:expect unsupported slice-expression

package main

// Slicing a string is left for increment 7.3 (D-036).

func main() {
	s := "hello"
	println(len(s[1:3]))
}
