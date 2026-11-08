// aether:expect run

package main

// float32 rounding: each float32 operation rounds to float32, and conversions
// to float32 round to nearest, ties to even, once, from the source value.

func narrow(x float64) float32 { return float32(x) }

func fromInt(x int64) float32 { return float32(x) }

func fromUint(x uint64) float32 { return float32(x) }

func main() {
	// Ties and halfway cases at 2^24.
	println(narrow(16777217), narrow(16777219), narrow(16777218.5), narrow(-16777217))
	// 0.1 rounds differently in float32 and float64.
	var a, b float32 = 0.1, 0.2
	println(a+b, a+b == 0.3, float64(a), float64(a+b), float64(a)+float64(b))
	println(narrow(0.1) == a, float64(narrow(0.1)) == 0.1)
	// Overflow to infinity and underflow to zero and subnormals.
	println(narrow(3.4028235e38), narrow(3.4028236e38), narrow(3.4028235677973366e38), narrow(1e39), narrow(-1e39))
	println(narrow(1.401298464324817e-45), narrow(7e-46), narrow(7.1e-46), narrow(1e-46), narrow(-1e-46))
	println(narrow(1.1754942e-38), narrow(5.877472e-39))
	// float32 arithmetic is not float64 arithmetic rounded at the end.
	var x float32 = 16777216
	var one float32 = 1
	println(x+one+one, x+(one+one), float32(float64(x)+1+1))
	var t float32 = 1e-8
	println(one+t-one, one-one+t)
	// A conversion from an integer rounds once: 2^60 + 2^36 + 1 is below
	// the midpoint for float64 but above it for float32 directly.
	n := int64(1)<<60 + 1<<36 + 1
	println(fromInt(n), narrow(float64(n)), fromInt(n) == narrow(float64(n)))
	println(fromInt(16777217), fromInt(-16777219), fromInt(9223372036854775807), fromInt(-9223372036854775808))
	println(fromUint(18446744073709551615), fromUint(1<<63+1<<39), fromUint(1<<63+1<<39+1), fromUint(16777217))
	// float32 to float64 is exact.
	println(float64(narrow(1.0/3)), float64(narrow(2.0/3)))
}
