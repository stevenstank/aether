// aether:expect run

package main

// Case values are evaluated only until one matches: a later case value
// that would panic is reached only when the earlier ones do not match.

func v(s string) string {
	println("value", s)
	return s
}

func main() {
	s := "abc"
	n := 5
	switch s {
	case v("abc"), s[:n]:
		println("first")
	}
	switch s {
	case v("x"), s[:n]:
		println("not reached")
	}
}
