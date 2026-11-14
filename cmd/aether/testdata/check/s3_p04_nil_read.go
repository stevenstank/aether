// aether:expect run

package main

// Reading through a nil pointer panics with gc's message, after earlier
// output.

type T struct {
	a [4]int
	n *T
}

func main() {
	t := &T{}
	t.n = t
	println(t.n.n.a[3], t.n == t)
	t.n.n = nil
	println(t.n.n == nil)
	println(t.n.n.a[1])
}
