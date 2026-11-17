// aether:expect run

package main

// A failed type assertion to a concrete type panics with gc's message,
// naming the dynamic type (D-041).

type Named interface{ Name() string }

type T struct{ n int }

func (t T) Name() string { return "t" }

func main() {
	var n Named = T{1}
	println(n.(T).n)
	println("before")
	println(n.(*T) == nil)
}
