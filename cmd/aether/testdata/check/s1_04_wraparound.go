// aether:expect run

package main

func main() {
	hi := 9223372036854775807
	lo := -hi - 1
	println(hi+1, lo-1, -lo, +lo)
	println(hi*2, lo*-1, hi*hi, lo*lo)
	x := 3037000500
	println(x * x)
}
