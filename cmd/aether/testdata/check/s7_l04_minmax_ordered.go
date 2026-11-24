// aether:expect run

package main

// min and max of floats and strings (Batch 1): NaNs, signed zeros, and
// infinities as gc computes them, a left fold over three or more
// operands, constants, and defined types.

type temp float64

type tag string

func zero(neg bool) float64 {
	z := 0.0
	if neg {
		z = -z
	}
	return z
}

func main() {
	nz, pz := zero(true), zero(false)
	inf := 1 / pz
	nan := pz / pz
	println(min(nz, pz), min(pz, nz), max(nz, pz), max(pz, nz))
	println(min(nan, 1.0), max(1.0, nan), min(inf, -inf), max(-inf, 2.5, inf))
	println(min(3.5, 1.0), max(3.5, 1.0), min(2.5, 1.5, 0.5, 1.0))
	var f, g float32 = 1.25, -2
	println(min(f, g), max(f, g, 0))
	println(min(temp(3), temp(-1.5)), max(temp(3), 4))
	s, t := "pear", "apple"
	println(min(s, t), max(s, t), min(s, t, "fig"), max("", s), min("", s), min(s))
	println(min(tag("b"), "a"), max(tag("ab"), "abc"))
	println(min("abc", "ab"), max("ab\xff", "ab"))
	m := "z"
	for _, w := range []string{"q", "zz", "b", "c"} {
		m = min(m, w)
	}
	println(m)
}
