// aether:expect run

package main

// Pointers into arrays and structs: p.f and p[i] through a pointer, len of
// a pointer to an array, &a[i] and &s.f, embedded pointers, elided & in
// composite literals, address-taken globals, copies and comparisons of
// structs holding pointers, conversions, and &*p.

type Point struct {
	X, Y int32
}

type Box struct {
	Min, Max Point
	Tag      *int
}

type Inner struct {
	A, B int16
}

type Outer struct {
	*Inner
	N [3]int64
}

var count int
var origin Point
var grid [4][4]uint8

func bump() *int {
	count++
	return &count
}

func area(b *Box) int32 {
	return (b.Max.X - b.Min.X) * (b.Max.Y - b.Min.Y)
}

func second(a *[3]int64) *int64 {
	return &a[1]
}

func cell(i, j int) *uint8 {
	return &grid[i][j]
}

func noisy() *[5]int {
	println("noisy")
	return nil
}

type Q struct {
	X, Y int32
}

func main() {
	b := &Box{Point{1, 2}, Point{4, 6}, nil}
	println(area(b))
	b.Max.X += 2
	pm := &b.Max
	pm.Y = 10
	println(area(b), b.Max.X, b.Max.Y)
	b.Tag = bump()
	*b.Tag += 5
	*bump() *= 3
	println(count, *b.Tag)

	po := &origin
	po.X, po.Y = 7, 8
	println(origin.X, origin.Y)

	*cell(1, 2) = 12
	c := cell(3, 3)
	*c = 33
	*c++
	println(grid[1][2], grid[3][3], cell(0, 0) == &grid[0][0], cell(0, 1) != cell(1, 0))

	o := Outer{Inner: &Inner{1, 2}}
	o.A = 3
	o.N[2] = 9
	e := second(&o.N)
	*e = 4
	o2 := o
	o2.B = 5
	o2.N[0] = 1
	println(o.A, o.B, o.N[0], o.N[1], o.N[2], o2.A, o2.N[0], o.Inner == o2.Inner)

	pts := [3]*Point{{1, 2}, 2: {Y: 5}}
	println(pts[0].X, pts[1] == nil, pts[2].Y)
	pa := &[4]int{10, 20, 30, 40}
	pa[1] += pa[3]
	println(pa[1], len(pa), len(noisy()))

	k := 6
	x, y := Box{Tag: &k}, Box{Tag: &k}
	println(x == y, x.Tag == y.Tag)
	y.Tag = new(int)
	*y.Tag = k
	println(x == y, *x.Tag, *y.Tag)

	q := (*Q)(&origin)
	q.Y = -1
	println(origin.Y, &*q == q)
}
