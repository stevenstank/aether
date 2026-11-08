// aether:expect run

package main

// Floats in calls, results, globals, constants, switch, and print: more
// float arguments than ABI-R1 passes in registers, mixed with integers,
// multiple results, recursion, package-level initializers, and typed and
// untyped constants, including float literals of integer type.

const third = 1.0 / 3
const third32 float32 = 1.0 / 3
const big = 1e300

var g64 = third * 3
var g32 float32 = third32 * 3
var counter float64
var scale = halve(10)

func halve(x float64) float64 {
	counter++
	return x / 2
}

func many(a float64, b float32, c int, d float64, e float32, f int8, g float64, h float32, i uint64, j float64) (float64, float32, int) {
	return a + float64(b) + d + float64(e) + g + float64(h) + j, b * e * h, c + int(f) + int(i)
}

func swap(x float64, y float32) (float64, float32) { return float64(y), float32(x) }

func sum(n int, acc float64) float64 {
	if n == 0 {
		return acc
	}
	return sum(n-1, acc+1/float64(n))
}

func classify(x float64) int {
	switch {
	case x != x:
		return 0
	case x < 0:
		return -1
	case x > 0:
		return 1
	}
	return 2
}

func pick(x float32) int {
	switch x {
	case 0.5:
		return 1
	case 1e-45, 2:
		return 2
	}
	return 3
}

func main() {
	println(g64, g32, third, third32, big, scale, counter)
	println(many(1, 2, 3, 4, 5, 6, 7, 8, 9, 10))
	p, q, r := many(0.1, 0.2, -1, 0.3, 0.4, -128, 0.5, 0.6, 1<<63, 0.7)
	println(p, q, r)
	s, t := swap(swap(1.5, 2.5))
	println(s, t)
	println(sum(1000, 0), sum(50000, 0))
	zero := 0.0
	println(classify(zero/zero), classify(-1e-300), classify(-zero), classify(5e-324), classify(1/zero))
	var z32 float32
	println(pick(0.5), pick(1e-45), pick(2), pick(-z32), pick(z32/z32))
	var i int = 1.0
	var u uint8 = 0x1p7
	const c int = 2.0 * 3
	var h = 0x1.8p1
	println(i, u, c, h, 1e6, 1e21, 123456789.0, 0.000123, 1e-7, float32(1e-7))
	print(1.5, -0.25, float32(0.1), true, 3)
	println()
	counter = 1e15
	counter++
	counter += 0.5
	println(counter)
	var x float64
	var y float32
	println(x, y, -x, -y, x == -x)
}
