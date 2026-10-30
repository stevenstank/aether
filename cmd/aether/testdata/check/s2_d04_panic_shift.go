// aether:expect run

package main

func main() {
	var s int32 = 2
	x := 1
	println(x << s)
	s -= 3
	println(x >> s)
}
