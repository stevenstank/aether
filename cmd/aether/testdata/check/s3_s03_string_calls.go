// aether:expect run

package main

// Strings through functions and globals: parameters and results, multiple
// results mixing strings with integers and floats, more arguments than
// ABI-R1 passes in registers, recursion, and package-level strings whose
// initializers run in dependency order, not declaration order.

var first = second
var second = name(2)
var zero string
var calls int
var lengths = len(first) + len(second) + len(third)
var third = "third"

func name(i int) string {
	calls++
	switch i {
	case 0:
		return ""
	case 1:
		return "one"
	}
	return "two ✓"
}

func pair(a, b string) (string, string) { return b, a }

func mixed(s string, i int, f float64) (int, string, float64, string) {
	return len(s) + i, s, f * 2, name(i)
}

func eight(a, b, c, d, e, f, g, h string) string {
	println(a, b, c, d, e, f, g, h)
	return h
}

func count(s string, i int) int {
	if i == len(s) {
		return 0
	}
	if s[i] == 108 {
		return 1 + count(s, i+1)
	}
	return count(s, i+1)
}

func setZero(s string) {
	zero = s
}

func main() {
	println(first, second, zero, len(zero), third, lengths, calls)
	a, b := pair("left", "right")
	println(a, b)
	println(pair(pair("x", "")))
	n, s, f, t := mixed("abc", 1, 1.25)
	println(n, s, f, t)
	println(mixed("", 0, -0.5))
	println(eight("1", "2", "", "4", "5\x00", "6", "7", "8"))
	println(count("hello, world", 0), count("", 0), count(second, 0))
	setZero(name(1))
	println(zero, len(zero), zero[2])
	setZero("")
	println(zero, len(zero), calls)
	var local string
	for i := 0; i < 3; i++ {
		local = name(i)
		print(local, len(local), " ")
	}
	println()
}
