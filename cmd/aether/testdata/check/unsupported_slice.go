// aether:expect unsupported slice-expression

package main

func main() {
	var a [2]int
	s := a[:]
	println(len(s))
}
