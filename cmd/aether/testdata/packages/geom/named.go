package geom

import "example.com/pk/base"

// Named embeds Point, and has a field other packages cannot name.
type Named struct {
	Point
	name string
}

func NewNamed(name string, x, y int) *Named { return &Named{Point{x, y}, name} }

func (n *Named) Name() string { return n.name }

// Table is a map from names to points.
var Table = map[string]Point{"one": {1, 1}}

// Must panics with a geom value unless ok.
func Must(ok bool) {
	defer base.Note("geom.Must")
	if !ok {
		panic(Square(5))
	}
}
