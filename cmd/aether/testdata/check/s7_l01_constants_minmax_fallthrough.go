// aether:expect run

package main

// iota and constants without a value, min and max of integers, and
// fallthrough (D-049, increment 11.5).

type Weekday int

const (
	Sunday Weekday = iota
	Monday
	Tuesday
	_
	Thursday
)

const (
	KB = 1 << (10 * (iota + 1))
	MB
	GB
)

const (
	a, b = iota, -iota
	c, d
)

var calls int

func next() int {
	calls++
	return calls
}

func grade(n int) string {
	s := ""
	switch {
	case n >= 90:
		s += "A"
		fallthrough
	case n >= 80:
		s += "B"
		fallthrough
	case n >= 70:
		s += "C"
	case n >= 0:
		s += "D"
		fallthrough
	default:
		s += "?"
	}
	return s
}

func classify(x uint8) int {
	r := 0
	switch x {
	case 0, 1:
		r++
		fallthrough
	case 2:
		r += 10
		if x == 0 {
			break
		}
		fallthrough
	case 3:
		r += 100
	}
	return r
}

func main() {
	println(Sunday, Monday, Tuesday, Thursday, KB, MB, GB, a, b, c, d)
	println(min(3, 1, 2), max(-5, -2, -9), min(int8(-128), 127), max(uint64(1)<<63, 5))
	x, y := 7, 3
	println(min(x, y), max(x, y), min(y-x, 0), max(x))
	m := min(next(), next(), next())
	println(m, calls)
	m = max(next()*0, next())
	println(m, calls)
	for _, n := range []int{95, 85, 75, 5, -1} {
		println(grade(n))
	}
	for i := range 5 {
		println(classify(uint8(i)))
	}
}
