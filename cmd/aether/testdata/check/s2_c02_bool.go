// aether:expect run

package main

// trace prints its argument, so the order and number of calls is visible.
func trace(x int, b bool) bool {
	println(x)
	return b
}

func xor(a, b bool) bool {
	return a != b
}

func main() {
	// Variables, constants, and zero values.
	var z bool
	t := true
	const c = 1 < 2
	const nc bool = !c
	println(z, t, c, nc, false)

	// Comparisons of int and bool.
	x, y := -3, 4
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
	println(-9223372036854775808 < x, x < 9223372036854775807, t == c, t != z)

	// ! && || and precedence.
	println(!t, t && z, t || z, !t || t && !z, (t || z) && z)
	println(xor(t, z), xor(t, t))

	// Short-circuit: the right operand runs only when it decides the result.
	println(trace(1, false) && trace(2, true))
	println(trace(3, true) && trace(4, false))
	println(trace(5, true) || trace(6, true))
	println(trace(7, false) || trace(8, true))
	println(trace(9, false) || trace(10, false) && trace(11, true))

	// Values computed before a short-circuit operand keep their value.
	a := 10
	println(a, trace(12, true) && a > 5, a+1)
	a = 20
	b := a > 15 && trace(13, true)
	a += 5
	println(a, b)

	// A short-circuit operand inside a call argument and a condition.
	if xor(trace(14, true) && trace(15, false), y > 0) {
		println(16)
	}
	for i := 0; i < 3 && trace(17+i, true); i++ {
	}
}
