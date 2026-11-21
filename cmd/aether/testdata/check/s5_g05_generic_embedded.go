// aether:expect run

// Embedded fields of instance types: selected and keyed by the generic
// type's name, in generic and non-generic code, with promotion through
// them, by value and by pointer.
package main

type E[T any] struct{ v T }

func (e E[T]) Get() T     { return e.v }
func (e *E[T]) Set(v T)   { e.v = v }
func (e E[T]) Pair() [2]T { return [2]T{e.v, e.v} }

type S struct {
	E[int]
	v string
}

type L[T any] struct {
	E[T]
	n int
}

func (l *L[T]) Reset(v T) { l.E = E[T]{v} }

type P struct {
	*E[string]
	n int
}

type N[T any] struct {
	L[T]
	P
}

func main() {
	s := S{E[int]{2}, "s"}
	s.E.v++
	s.Set(s.Get() * 10)
	println(s.E.v, s.Get(), s.v, s.Pair()[1])
	k := S{E: E[int]{7}, v: "k"}
	println(k.E.v, k.v)
	var l L[int]
	l.Reset(5)
	println(l.E.v, l.Get(), l.v, l.n)
	p := P{E: &E[string]{"x"}}
	p.Set("y")
	println(p.E.v, p.Get(), p.v)
	n := N[bool]{L: L[bool]{E: E[bool]{true}}, P: p}
	n.L.E.v = false
	println(n.L.E.v, n.P.E.v, n.L.Get(), n.P.Get())
	var np P
	defer func() { println("recovered:", recover() != nil) }()
	println(np.Get())
}
