// aether:expect run

package main

// A method promoted through a nil embedded pointer: a pointer method of
// the embedded type receives nil without a panic, but a method of a type
// embedded in the pointed-to struct panics when the path follows the nil
// pointer to reach it.

type Inner struct{ n int }

func (i *Inner) Nil() bool { return i == nil }

func (i Inner) N() int { return i.n }

type Mid struct {
	Inner
}

type Outer struct {
	*Mid
	k int
}

func main() {
	var o Outer
	println(o.k, o.Mid == nil)
	println("path")
	println(o.Nil())
	println("not reached")
}
