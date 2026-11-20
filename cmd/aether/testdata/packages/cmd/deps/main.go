// A program of several packages: initialization order, names declared in
// more than one package, methods, interfaces, func values, closures,
// generics, and goroutines across packages.
package main

import (
	"example.com/pk/alpha"
	"example.com/pk/base"
	"example.com/pk/geom"
)

var start = geom.Count()

func init() { base.Note("main.init") }

type Kind int

func (Kind) String() string { return "main kind" }

type stringer interface{ String() string }

func main() {
	for _, s := range base.Log {
		println(s)
	}
	println(start, alpha.Count(), base.Count, base.Max)

	ks := []stringer{Kind(1), alpha.Kind(2), base.Kind(3)}
	for _, k := range ks {
		switch k := k.(type) {
		case alpha.Kind:
			println("alpha", int(k), k.String())
		case base.Kind:
			println("base", int(k), k.String())
		default:
			println(k.String())
		}
	}

	p := geom.Point{X: 1, Y: 2}
	q := p.Add(geom.Unit)
	geom.Grow(&q)
	add := p.Add
	scale := (*geom.Point).Scale
	scale(&p, 3)
	println(q.X, q.Y, add(p).X, p.Y, geom.Origin.X)
	area := 0
	for _, s := range geom.Shapes {
		area += s.Area()
	}
	var s geom.Shape = geom.Square(4)
	if sq, ok := s.(geom.Square); ok {
		println("square", int(sq), s.Area())
	}
	println(area, geom.OS(), geom.Arch())

	total := alpha.Total
	println(total([]int{1, 2, 3}), alpha.Mean([]float64{1, 2}))
	c := alpha.Counter()
	c()
	println(c(), alpha.Counter()())
	sum := 0
	for v := range alpha.Gen(5) {
		sum += v
	}
	println(sum)

	defer func() {
		r := recover()
		if k, ok := r.(alpha.Kind); ok {
			println("recovered", k.String(), int(k))
		}
	}()
	panic(alpha.Kind(7))
}
