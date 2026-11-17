// aether:expect run

package main

// A method value with a value receiver through a nil pointer panics when
// it is evaluated; one with a pointer receiver does not, and its method
// may be called on nil (D-039).

type T struct{ n int }

func (t *T) IsNil() bool { return t == nil }

func (t T) Get() int { return t.n }

func main() {
	var p *T
	isNil := p.IsNil
	println(isNil())
	byPtr := (*T).IsNil
	println(byPtr(nil))
	get := p.Get
	println("not reached", get())
}
