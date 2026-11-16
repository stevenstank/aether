// aether:expect unsupported method

package main

// Aether names a method's function T.M, and its own generated functions
// aether.streq and the like, so it rejects methods of a type named aether.

type aether struct{ s string }

func (a aether) streq(t string) bool { return a.s == t }

func main() {
	println(aether{"x"}.streq("x"))
}
