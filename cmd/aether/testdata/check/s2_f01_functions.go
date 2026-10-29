// aether:expect run

package main

// Parameters and a result, with operands evaluated left to right.
func sub3(a, b, c int) int {
	return a - b*c
}

// A call that prints, so the order of calls is visible.
func trace(x int) int {
	println(x)
	return x
}

// Blank and unnamed parameters.
func second(_ int, y int) int { return y }

func ignore(int) {}

func noResult(x int) {
	println(x, x)
	return
	println(0)
}

func nested(x int) int {
	{
		return x * 2
	}
}

func main() {
	x := sub3(1, 2, 3)
	println(x, sub3(x, x, -1))
	println(trace(1) + trace(2)*trace(3))
	noResult(sub3(trace(4), 1, 1))
	ignore(trace(5))
	trace(6)
	println(second(7, 8), nested(9), nested(nested(-1)))
	println(sub3(9223372036854775807, -1, 1))
	return
	println(10)
}
