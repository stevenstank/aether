// aether:expect run

package main

// A string's high bound beyond its length panics with gc's message.

func main() {
	s := "abc"
	n := 5
	println(s[1:3])
	println(s[:n])
}
