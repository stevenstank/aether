// aether:expect run

package main

// == and != on arrays and structs: field by field and element by element,
// floats by IEEE 754 (so NaN is unequal to itself and -0 equals +0), blank
// fields ignored, and large arrays compared in a loop.

type P struct {
	x float64
	_ int
	b bool
	n int8
}

type Q struct {
	p  P
	a  [3]uint16
	f  float32
	ok bool
}

func nan() float64 {
	z := 0.0
	return z / z
}

func main() {
	var a, b P
	println(a == b, a != b)
	a.x = 1
	println(a == b, a != b)
	b.x = 1
	b.n = -1
	println(a == b, P{x: 1, n: -1} == b)
	zero := 0.0
	neg := P{x: -zero}
	println(neg == P{}, neg.x)
	n := P{x: nan()}
	m := n
	println(n == m, n != m, n.x == m.x)

	var q, r Q
	println(q == r)
	r.a[2] = 7
	println(q == r, q.a == r.a, q.p == r.p)
	q.a[2] = 7
	println(q == r)

	var x, y [100]int
	println(x == y)
	y[99] = 1
	println(x == y, x != y)
	x[99] = 1
	println(x == y)
	var e1, e2 [4]struct{}
	println(e1 == e2, [0]int{} == [0]int{})
	fs := [2]float32{1, 2}
	gs := fs
	gs[1] = 2.0000002
	println(fs == gs, fs != [2]float32{1, 2})
}
