// aether:expect run

package main

// Struct types: zero values, keyed and positional literals, fields of every
// scalar type, nested and embedded structs with promoted fields, blank
// fields, local types, and copies that do not share memory.

type Point struct {
	X, Y int
}

type Mixed struct {
	b   bool
	i8  int8
	u16 uint16
	i32 int32
	u64 uint64
	f32 float32
	f64 float64
	s   string
	_   int16
	u8  uint8
}

type Named struct {
	Point
	Name  string
	Inner struct{ A, B int8 }
}

func main() {
	var z Point
	println(z.X, z.Y)
	p := Point{1, 2}
	q := Point{Y: 5}
	println(p.X, p.Y, q.X, q.Y)
	// A copy is a new value.
	r := p
	r.X = 100
	println(p.X, r.X)
	p = q
	q.Y = -7
	println(p.Y, q.Y)

	var m Mixed
	println(m.b, m.i8, m.u16, m.i32, m.u64, m.f32, m.f64, m.s, len(m.s), m.u8)
	m = Mixed{true, -128, 65535, -2147483648, 18446744073709551615, 1.5, -0.25, "héllo", 3, 255}
	println(m.b, m.i8, m.u16, m.i32, m.u64, m.f32, m.f64, m.s, len(m.s), m.u8)
	m.i8--
	m.u16++
	m.u8 += 2
	m.f64 *= 4
	m.s = "x"
	println(m.i8, m.u16, m.u8, m.f64, m.s, m.s[0])

	n := Named{Point{3, 4}, "n", struct{ A, B int8 }{-1, 2}}
	println(n.X, n.Y, n.Point.X, n.Name, n.Inner.A, n.Inner.B)
	n.X = 30
	n.Inner.B *= 50
	o := n
	o.Point = Point{}
	o.Inner.A = 9
	println(n.X, n.Inner.B, n.Inner.A, o.X, o.Y, o.Inner.A, o.Name)

	type Local struct {
		a [0]int
		e struct{}
		v float32
	}
	var l Local
	l.v = 0.1
	k := Local{v: 2}
	println(l.v, k.v, len(l.a))
}
