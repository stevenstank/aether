// aether:expect run

package main

func main() {
	a := 6
	b := 7
	var c int = a*b - 2
	c += 2
	{
		a := -a
		println(a, c)
	}
	big := 9223372036854775807
	big++
	println(a, big)
}
