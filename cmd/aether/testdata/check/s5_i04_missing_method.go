// aether:expect run

package main

// An assertion to an interface whose method the dynamic type lacks panics
// with gc's message, naming the first missing method (D-041).

type A interface{ M() int }

type B interface {
	M() int
	N() int
	K() int
}

type T struct{ s []string }

func (T) M() int { return 1 }
func (T) N() int { return 2 }

func main() {
	var a A = T{}
	b, ok := a.(B)
	println(ok, b == nil)
	println(a.(B).M())
}
