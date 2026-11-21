// aether:expect run

package main

// A shift's count may be an untyped constant of any kind representable as
// a uint (spec §Operators): a float, a complex number, or an integer above
// int64's range. It is a uint constant, as in gc.

const M = ^uint(0)

type P struct{ v int }

func main() {
	x := 1
	println(x<<1., x<<(1+0i), x<<0i, x>>(M+0), x<<(M+0.), x<<(M+0.+0i))
	println(x<<(1.<<x), x<<((1+0i)<<x), x<<(0i<<x))
	y := 3
	y <<= 2.
	y >>= 1 + 0i
	p := &P{5}
	p.v <<= 3.
	a := []int{7}
	a[0] >>= M + 0
	println(y, p.v, a[0])
}
