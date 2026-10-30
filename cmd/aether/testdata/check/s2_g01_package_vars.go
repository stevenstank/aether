// aether:expect run

package main

// Package-level variables of every kind of type, with and without an
// initializer, and constants, typed and untyped. A global is never read in
// the same expression as a call that assigns it: that order is unspecified
// (SEMANTICS.md §Evaluation order).

var count int

var (
	small int8 = -128
	big   uint64
	flag  bool = true
	b     byte = 200
)

var r, s = 3, uint16(65535)

const k = 1 << 40

const typed int16 = -7

const yes = !false

func bump() {
	count++
	small--
	big -= 1
	flag = !flag
}

func add(n int) int {
	count += n
	return count
}

func main() {
	println(count, small, big, flag, b, r, s, k, typed, yes)
	bump()
	println(count, small, big, flag)
	bump()
	println(count, small, big, flag)
	x := add(5)
	println(x)
	x = add(k)
	println(x, count)
	// A local shadows the global within its block only.
	{
		count := -1
		count *= 3
		println(count)
	}
	println(count)
	s++
	b += 100
	r <<= 61
	println(s, b, r, r>>60)
	for i := 0; i < 3; i++ {
		count = count/2 + i
	}
	println(count)
}
