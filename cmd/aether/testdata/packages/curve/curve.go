// Package curve has generic code that the monomorphizer once rewrote
// wrongly (D-057): a generic function that calls non-generic code
// returning an instance type, kept for importers; and an instance of its
// own generic code whose type argument is of a package that only another
// file imports.
package curve

type Point[P any] interface{ Get() P }

type IP struct{ v int }

func (p *IP) Get() *IP { return p }

type Curve[P Point[P]] struct{ name string }

func C1() *Curve[*IP] { return &Curve[*IP]{"c1"} }

func octets[P Point[P]](c *Curve[P], h int) int { return len(c.name) + h }

// PCT calls octets with C1's curve, whatever its own.
func PCT[P Point[P]](c *Curve[P]) int {
	f := func() int { return octets(C1(), 2) }
	return f() + len(c.name)
}

// newHash is called, in cast.go, with a type argument of package strings,
// which this file does not import.
func newHash[H any](f func(string) H) H { return f("hash") }
