// aether:expect run

package main

// A nil *T calling T's own value method through the wrapper of an itab or
// a method expression panics as gc's runtime.panicwrap does, with a
// runtime.Error of its own message (go.dev/issue/19040). A promoted
// method's wrapper has no such check: the nil pointer is dereferenced.

type T int

func (t T) F() int { return int(t) }

type G[X any] struct{ x X }

func (g G[X]) F() int { return 1 }

type S struct{ n int }

func (s S) F() int { return s.n }

type E struct{ S }

type I interface{ F() int }

var (
	t  *T
	g  *G[string]
	e  *E
	it I = t
	ig I = g
	ie I = e
)

//go:noinline
func call(i I) int { return i.F() }

func try(name string, f func()) {
	defer func() {
		r := recover()
		_, rt := r.(interface{ RuntimeError() })
		println(name, r.(error).Error(), rt)
	}()
	f()
}

func main() {
	try("iface", func() { call(it) })
	try("generic", func() { call(ig) })
	try("promoted", func() { call(ie) })
	try("methodexpr", func() { (*T).F(t) })
	v := T(4)
	println(call(&v), (*T).F(&v))
	call(it)
}
