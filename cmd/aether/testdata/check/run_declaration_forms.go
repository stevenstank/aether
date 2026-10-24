// aether:expect run

package main

func main() {
	var (
		a int
		b int = 0x1F
		c     = 0o17 + 0b101 + 017 + 1_000
	)
	var d, e = a + b, c
	const (
		k     = 3
		n int = k * k
	)
	const m = -9223372036854775808
	f := +(d - (e * n))
	println(a, b, c, d, e, f, k, n, m)
}
