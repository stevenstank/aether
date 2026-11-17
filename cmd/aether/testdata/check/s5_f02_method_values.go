// aether:expect run

package main

// Method values and method expressions (D-039): a method value binds a
// copy of a value receiver, or the pointer of a pointer receiver, when it
// is evaluated; through the implicit & and *, and methods promoted through
// embedded fields and pointers. Method expressions T.M and (*T).M take the
// receiver as their first argument.

type Counter struct {
	n    int
	name string
}

func (c Counter) Get() int { return c.n }

func (c *Counter) Inc() int {
	c.n++
	return c.n
}

func (c Counter) Add(k int) Counter {
	c.n += k // the receiver is a copy
	return c
}

func (c Counter) Both(k int) (int, string) { return c.n + k, c.name }

type Vec [3]int

func (v Vec) Sum() int { return v[0] + v[1] + v[2] }

func (v *Vec) Scale(k int) {
	for i := 0; i < 3; i++ {
		v[i] *= k
	}
}

type Inner struct{ c Counter }

type Outer struct {
	Counter
	*Vec
	label string
}

func call(f func() int) int { return f() }

var global Counter
var bound = global.Inc

func main() {
	c := Counter{n: 1, name: "c"}
	get := c.Get // copies c now
	inc := c.Inc // &c
	c.n = 10
	println(get(), inc(), inc(), c.n, get())

	add := c.Add
	c.n = 0
	println(add(5).n, add(5).n, c.n)

	both := c.Both
	println(both(1))

	p := &c
	pg := p.Get // copies *p now
	pi := p.Inc
	p.n = 100
	println(pg(), pi(), c.n)

	v := Vec{1, 2, 3}
	sum := v.Sum
	scale := v.Scale
	scale(2)
	println(sum(), v.Sum(), call(v.Sum))

	o := Outer{Counter{n: 7, name: "o"}, &v, "outer"}
	og := o.Get
	oi := o.Inc
	os := o.Sum
	osc := o.Scale
	oi()
	osc(10)
	println(og(), o.n, os(), v[0], call(o.Get))

	println(bound(), bound(), global.n)

	// Method expressions.
	getE := Counter.Get
	addE := Counter.Add
	incE := (*Counter).Inc
	getP := (*Counter).Get
	println(getE(c), addE(c, 2).n, incE(&c), getP(&c), c.n)
	sumE := Vec.Sum
	scaleE := (*Vec).Scale
	scaleE(&v, 2)
	println(sumE(v), v[2])
	ogE := Outer.Get
	oiE := (*Outer).Inc
	osE := Outer.Sum
	oiE(&o)
	println(ogE(o), osE(o), o.n)

	fs := [](func() int){c.Get, p.Inc, o.Get, v.Sum}
	for i := 0; i < len(fs); i++ {
		print(fs[i](), " ")
	}
	println()
	var in Inner
	ic := in.c.Inc
	ic()
	println(in.c.n)
}
