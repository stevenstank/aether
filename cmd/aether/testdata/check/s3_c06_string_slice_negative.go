// aether:expect run

package main

// A negative low bound panics without the high bound in the message.

func main() {
	s := "abcdef"
	i := -1
	println(s[2:])
	println(s[i:])
}
