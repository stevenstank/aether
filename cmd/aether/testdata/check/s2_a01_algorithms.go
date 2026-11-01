// aether:expect run

package main

// Small algorithms that combine the features of Phase 4: functions with
// multiple results, recursion, loops, switch, every integer width, and
// package-level state.

var steps int // total Collatz steps, across calls

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

func isPrime(n int) bool {
	switch {
	case n < 2:
		return false
	case n < 4:
		return true
	case n%2 == 0:
		return false
	}
	for d := 3; d*d <= n; d += 2 {
		if n%d == 0 {
			return false
		}
	}
	return true
}

func collatz(n uint64) int {
	k := 0
	for n != 1 {
		switch n % 2 {
		case 0:
			n /= 2
		default:
			n = 3*n + 1
		}
		k++
	}
	steps += k
	return k
}

func fib(n int) (uint64, uint64) {
	if n == 0 {
		return 0, 1
	}
	a, b := fib(n / 2)
	c := a * (2*b - a)
	d := a*a + b*b
	if n%2 == 0 {
		return c, d
	}
	return d, c + d
}

func popcount(x uint64) int {
	n := 0
	for x != 0 {
		x &= x - 1
		n++
	}
	return n
}

func isqrt(n uint64) uint64 {
	if n < 2 {
		return n
	}
	// The first step rounds n/2 up without computing n+1, which wraps
	// for the largest n.
	x := n
	y := x/2 + x%2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}

// powmod computes b**e mod m for m below 2**32, so products fit.
func powmod(b, e, m uint64) uint64 {
	r := uint64(1)
	b %= m
	for e > 0 {
		if e&1 == 1 {
			r = r * b % m
		}
		b = b * b % m
		e >>= 1
	}
	return r
}

func main() {
	println(gcd(1071, 462), gcd(-48, 18), gcd(0, 7), gcd(-9223372036854775807, 3))
	primes := 0
	last := 0
	for n := 0; n < 1000; n++ {
		if isPrime(n) {
			primes++
			last = n
		}
	}
	println(primes, last, isPrime(2147483647))
	// steps is read in a statement of its own: its order relative to
	// the calls that assign it would be unspecified (SEMANTICS.md V-4).
	println(collatz(27), collatz(97), collatz(871))
	println(steps)
	println(fib(10))
	println(fib(93))
	println(fib(94))
	println(popcount(0), popcount(0xffffffffffffffff), popcount(0x8000000000000001))
	println(isqrt(0), isqrt(15), isqrt(16), isqrt(1<<62), isqrt(18446744073709551615))
	println(powmod(2, 10, 1000), powmod(3, 1000000, 1000000007), powmod(7, 0, 13))
	var i8 int8 = 100
	var i16 int16 = 30000
	var u16 uint16 = 65535
	print(i8+i8, i16*2, u16+1)
	println()
}
