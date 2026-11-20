package geom

import (
	"example.com/pk/alpha"
	"example.com/pk/base"
)

type Point struct{ X, Y int }

func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }
func (p *Point) Scale(k int)      { p.X *= k; p.Y *= k }

type Shape interface{ Area() int }

type Rect struct{ Min, Max Point }

func (r Rect) Area() int { return (r.Max.X - r.Min.X) * (r.Max.Y - r.Min.Y) }

type Square int

func (s Square) Area() int { return int(s * s) }

var Origin Point
var Unit = Point{base.Max, alpha.Count()}
var Shapes = []Shape{Rect{Origin, Unit}, Square(3)}

var count int

func init() {
	base.Note("geom.init")
	count = len(Shapes)
}

func Count() int    { return count }
func Grow(p *Point) { p.Scale(2) }
