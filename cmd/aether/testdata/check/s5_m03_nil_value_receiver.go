// aether:expect run

package main

// A value method called through a nil pointer copies *p, so it panics at
// the call, before the method runs; a pointer method on nil does not.

type T struct{ a, b int }

func (t T) Sum() int {
	println("not reached")
	return t.a + t.b
}

func (t *T) Ok() bool { return t == nil }

func main() {
	var p *T
	println(p.Ok())
	println("before")
	println(p.Sum())
	println("not reached either")
}
