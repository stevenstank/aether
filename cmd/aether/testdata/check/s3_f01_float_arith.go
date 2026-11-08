// aether:expect run

package main

// Arithmetic and comparison of float64 and float32 values, through function
// parameters, on the IEEE 754 special values: signed zeros, infinities, NaN,
// subnormals, and the extremes. Every comparison is printed for every pair.

func ops64(x, y float64) {
	println(x+y, x-y, x*y, x/y, -x)
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
}

func ops32(x, y float32) {
	println(x+y, x-y, x*y, x/y, -x)
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
}

func each64(x float64, zero float64) {
	inf := 1 / zero
	nan := zero / zero
	ops64(x, zero)
	ops64(x, -zero)
	ops64(x, 1)
	ops64(x, -2.5)
	ops64(x, inf)
	ops64(x, -inf)
	ops64(x, nan)
	ops64(x, 5e-324)
	ops64(x, 1.7976931348623157e308)
	ops64(x, x)
}

func each32(x float32, zero float32) {
	inf := 1 / zero
	nan := zero / zero
	ops32(x, zero)
	ops32(x, -zero)
	ops32(x, 1)
	ops32(x, -2.5)
	ops32(x, inf)
	ops32(x, -inf)
	ops32(x, nan)
	ops32(x, 1e-45)
	ops32(x, 3.4028235e38)
	ops32(x, x)
}

func main() {
	zero := 0.0
	inf := 1 / zero
	each64(0, zero)
	each64(-zero, zero)
	each64(1, zero)
	each64(0.1, zero)
	each64(-3, zero)
	each64(inf, zero)
	each64(-inf, zero)
	each64(zero/zero, zero)
	each64(5e-324, zero)
	each64(2.2250738585072014e-308, zero)
	each64(1.7976931348623157e308, zero)
	var z32 float32
	each32(0, z32)
	each32(-z32, z32)
	each32(1, z32)
	each32(0.1, z32)
	each32(-3, z32)
	each32(1/z32, z32)
	each32(z32/z32, z32)
	each32(1e-45, z32)
	each32(1.1754944e-38, z32)
	each32(3.4028235e38, z32)

	// Rounding of each operation, and of compound assignment.
	a, b := 0.1, 0.2
	println(a+b, a+b == 0.3, (a+b)+0.3, a+(b+0.3), a*3, 1/(a*3))
	s := 0.0
	for i := 0; i < 10; i++ {
		s += a
	}
	println(s, s == 1, s-1)
	t := 1.0
	t -= 1e-16
	t *= 3
	t /= 7
	t++
	t--
	println(t)
	var f float32 = 16777216
	f++
	println(f, f+2, f-1)
	m := 1e308
	println(m*10, -m*10, m*10-m*10, (m*10)*zero, 5e-324/2, 2.2250738585072014e-308/4)
	var mf float32 = 3e38
	println(mf*10, mf*mf*z32, mf+mf)
	println(inf+inf, inf-inf, inf*0, -inf*-1, 1/inf, -1/inf)
}
