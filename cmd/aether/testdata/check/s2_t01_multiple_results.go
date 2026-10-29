// aether:expect run

package main

func swap(a, b int) (int, int) {
	return b, a
}

// divide returns the quotient and remainder of a by b > 0, for a >= 0, by
// repeated subtraction (/ and % come in increment 4.5).
func divide(a, b int) (int, int, bool) {
	q := 0
	for a >= b {
		a -= b
		q++
	}
	return q, a, a == 0
}

// fib returns fib(n) and fib(n+1).
func fib(n int) (int, int) {
	if n == 0 {
		return 0, 1
	}
	a, b := fib(n - 1)
	return b, a + b
}

func forward() (int, int) {
	return swap(1, 2)
}

func show(q, r int, exact bool) {
	println(q, r, exact)
}

func main() {
	x, y := swap(3, 4)
	println(x, y)
	x, y = swap(x, y)
	println(x, y)
	var q, r, exact = divide(17, 5)
	println(q, r, exact)
	show(divide(20, 4))
	println(fib(90))
	a, b := forward()
	println(a, b)

	// Both calls happen before either result is used.
	c, d := fib(10)
	e, f := fib(20)
	println(c, d, e, f)

	// A call whose results are discarded.
	divide(1, 1)

	// Results used across a branch.
	if g, h := swap(-1, 9223372036854775807); g > h || g == 0 {
		println(g)
	} else {
		println(h)
	}
	ok, n := true, 0
	n, ok = swap2(n, ok && n == 0)
	println(n, ok)
}

func swap2(n int, b bool) (int, bool) {
	return n + 1, !b
}
