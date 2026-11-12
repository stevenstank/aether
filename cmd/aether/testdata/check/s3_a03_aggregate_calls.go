// aether:expect run

package main

// Arrays and structs as parameters and results: passed and returned by
// value, so a callee's changes to its parameter do not reach the caller;
// several results mixing them with scalars; results used directly as
// operands; and recursion that keeps an array per call.

type Vec struct {
	X, Y, Z float64
}

type Pair struct {
	A [2]int
	B string
}

func add(a, b Vec) Vec {
	return Vec{a.X + b.X, a.Y + b.Y, a.Z + b.Z}
}

func scale(v Vec, k float64) Vec {
	v.X *= k
	v.Y *= k
	v.Z *= k
	return v
}

func bump(a [3]int) [3]int {
	a[0]++
	return a
}

func split(p Pair) (int, [2]int, string, bool) {
	p.A[0] = -p.A[0]
	return p.A[0], p.A, p.B, len(p.B) > 1
}

func make8(n int) [8]int {
	var r [8]int
	for i := 0; i < 8; i++ {
		r[i] = n * i
	}
	return r
}

func sum(a [40]int) int {
	s := 0
	for i := 0; i < len(a); i++ {
		s += a[i]
	}
	return s
}

func fill(depth int, a [4]int) [4]int {
	a[depth%4] += depth
	if depth == 0 {
		return a
	}
	b := fill(depth-1, a)
	b[0] += a[0]
	return b
}

// two returns an array copied in a loop, then a second array: the second
// result's value must survive the first one's copy loop.
func two(n int) ([20]int, [2]int8, int) {
	var a [20]int
	a[19] = n
	return a, [2]int8{int8(n), -int8(n)}, n * 2
}

func main() {
	v := Vec{1, 2, 3}
	w := scale(v, 2)
	println(v.X, v.Y, v.Z, w.X, w.Y, w.Z)
	u := add(v, add(w, Vec{Z: 0.5}))
	println(u.X, u.Y, u.Z, add(v, v).Y, scale(v, -1).Z)

	a := [3]int{1, 2, 3}
	b := bump(a)
	println(a[0], b[0], bump(bump(a))[0], bump(b)[1])

	p := Pair{[2]int{5, 6}, "pair"}
	n, arr, s, long := split(p)
	println(n, arr[0], arr[1], s, long, p.A[0])
	n, _2, s, long := split(Pair{})
	println(n, _2[1], s, long)

	println(make8(3)[7], len(make8(1)))
	var big [40]int
	for i := 0; i < 40; i++ {
		big[i] = i
	}
	println(sum(big), big[39])
	r := fill(6, [4]int{})
	println(r[0], r[1], r[2], r[3])
	big2, small, m := two(7)
	println(big2[19], small[0], small[1], m)
}
