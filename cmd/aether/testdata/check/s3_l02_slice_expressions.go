// aether:expect run

package main

// Slicing slices, arrays, pointers to arrays, and global arrays, with two
// and three indices: the new length and capacity, and sharing of the array.

var g [6]int16

type T struct {
	a [4]int
	s []int
}

func window(a *[8]int, lo, hi int) []int {
	return a[lo:hi]
}

func main() {
	a := [5]int{1, 2, 3, 4, 5}
	s := a[1:4]
	s[0] = 20
	println(a[1], len(s), cap(s), s[2])
	t := s[1:2:3]
	t[0] = 30
	println(a[2], len(t), cap(t), len(s[:0]), cap(s[2:]))
	u := a[:]
	v := u[3:]
	v[1] = 50
	println(len(u), cap(v), a[4], len(a[5:]), cap(a[:2:2]))
	var big [8]int
	for i := 0; i < 8; i++ {
		big[i] = i * i
	}
	w := window(&big, 2, 6)
	w[3] = -1
	println(len(w), cap(w), w[0], big[5])
	x := g[2:4:6]
	x[1] = 7
	println(g[3], len(x), cap(x))
	var p T
	p.s = p.a[1:3]
	p.s[1] = 9
	pp := &p
	q := pp.a[:]
	println(p.a[2], len(q), q[2], len(pp.s), cap(pp.s))
	r := []int{0, 1, 2, 3, 4, 5}
	lo, hi, mx := uint8(1), int32(3), int64(5)
	y := r[lo:hi:mx]
	println(len(y), cap(y), y[0], len(r[hi:]), len(r[:lo]))
}
