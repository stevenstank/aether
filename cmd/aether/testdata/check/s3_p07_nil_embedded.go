// aether:expect run

package main

// A field promoted through a nil embedded pointer panics; so does taking
// the address of a field through a nil pointer.

type Inner struct {
	a, b int
}

type Outer struct {
	*Inner
	c int
}

func main() {
	o := Outer{Inner: &Inner{1, 2}}
	println(o.a, o.b, o.c)
	o.Inner = nil
	o.c = 3
	println(o.c)
	p := &o.b
	println(p == nil)
}
