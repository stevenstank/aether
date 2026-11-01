// aether:expect run

package main

var calls int

// v prints its argument when called, to show which case values are
// evaluated.
func v(x int) int {
	calls++
	println(x)
	return x
}

func classify(x int) int {
	switch {
	case x < 0:
		return -1
	case x == 0:
		return 0
	default:
		return 1
	}
}

func kind(x int8) int {
	k := 0
	switch x {
	default:
		k = 9
	case -128, 127:
		k = 1
	case 0:
	case 1, 2, 3:
		k = 3
	}
	return k
}

func main() {
	// Case values are evaluated left to right and top to bottom, and only
	// until one matches.
	for i := 0; i < 5; i++ {
		switch i {
		case v(10), v(1):
			println(1)
		case v(2), v(3):
			println(23)
		default:
			println(-1)
		}
	}
	println(calls)

	// The tag is evaluated once.
	calls = 0
	switch v(7) {
	case 5, 6, 7:
		println(true)
	}
	println(calls)

	for x := -2; x <= 2; x++ {
		println(classify(x))
	}
	println(kind(-128), kind(127), kind(0), kind(2), kind(4), kind(-1))

	// break leaves the switch, continue the loop.
	for i := 0; i < 6; i++ {
		switch i % 3 {
		case 0:
			continue
		case 1:
			if i > 3 {
				break
			}
			println(i, 1)
		}
		println(i)
	}

	// An init statement, a shadowed tag, and nested switches.
	x := 3
	switch x := x * 2; x {
	case 6:
		switch y := x - 1; {
		case y > 4:
			println(x, y)
		}
		x++
		println(x)
	}
	println(x)

	// Unsigned values above the int64 range, a bool tag, and an empty switch.
	var u uint64 = 1 << 63
	switch u {
	case 1:
		println(1)
	case 1 << 63:
		println(u)
	}
	switch b := x > 2; b {
	case false:
		println(false)
	case true:
		println(b)
	}
	switch x {
	}
	switch {
	default:
	}
	println(classify(-5) + classify(5))
}
