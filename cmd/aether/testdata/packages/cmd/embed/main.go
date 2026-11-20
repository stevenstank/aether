// Package-level variables of other packages read, written, and addressed;
// embedded types and interfaces across packages; constants of another
// package in types; defer and recover across packages.
package main

import (
	"example.com/pk/alpha"
	"example.com/pk/base"
	"example.com/pk/geom"
)

type Circle struct {
	*geom.Named
	r int
}

func (c Circle) Area() int { return 3 * c.r * c.r }

var grid [base.Max]geom.Point

func move(p *geom.Point, dx int) { p.X += dx }

func main() {
	base.Log = nil
	geom.Origin.X = 4
	move(&geom.Origin, 2)
	o := &geom.Origin
	o.Y = -1
	println(geom.Origin.X, geom.Origin.Y, len(grid), len(base.Log))

	c := Circle{geom.NewNamed("c", 1, 2), 2}
	c.Scale(10)
	var s geom.Shape = c
	println(c.Name(), c.X, c.Point.Y, s.Area())
	shapes := append(geom.Shapes, s, geom.Square(2))
	for i, sh := range shapes {
		switch sh := sh.(type) {
		case Circle:
			println(i, "circle", sh.r)
		case geom.Rect:
			println(i, "rect", sh.Max.X, sh.Max.Y)
		default:
			println(i, sh.Area())
		}
	}

	geom.Table["two"] = geom.Point{X: 2, Y: 2}
	println(len(geom.Table), geom.Table["two"].X, geom.Table["three"].Y)
	grid[3] = geom.Unit.Add(geom.Point{X: 1})
	println(grid[3].X, grid[3].Y)

	k := alpha.Kind(4)
	f := k.String
	println(f(), base.Kind(0).String())

	geom.Must(true)
	func() {
		defer func() {
			r := recover()
			sq, ok := r.(geom.Square)
			println("recovered", ok, int(sq), sq.Area())
		}()
		geom.Must(false)
	}()
	for _, s := range base.Log {
		println(s)
	}
}
