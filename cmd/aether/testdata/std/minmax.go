package main

import (
	"fmt"
	"math"
)

type celsius float64
type name string


func f64(x float64) float64 { return x }

func bits(x float64) string { return fmt.Sprintf("%016x", math.Float64bits(x)) }
func bits32(x float32) string { return fmt.Sprintf("%08x", math.Float32bits(x)) }

func main() {
	nan := math.NaN()
	negNaN := math.Float64frombits(0xfff8000000000001)
	pz, nz := f64(0), math.Copysign(0, -1)
	inf := math.Inf(1)
	vals := []float64{1, -1, 2.5, pz, nz, nan, negNaN, inf, -inf, math.SmallestNonzeroFloat64}
	for _, x := range vals {
		for _, y := range vals {
			fmt.Println(bits(x), bits(y), "min", bits(min(x, y)), "max", bits(max(x, y)))
		}
	}
	for _, x := range vals {
		for _, y := range vals {
			for _, z := range vals[:6] {
				fmt.Println(bits(min(x, y, z)), bits(max(z, x, y)))
			}
		}
	}
	var a, b float32 = float32(nz), 0
	n32 := float32(nan)
	fmt.Println(bits32(min(a, b)), bits32(max(a, b)), bits32(min(b, a)), bits32(max(b, a)), bits32(min(n32, -1)), bits32(max(float32(-2), n32)))
	fmt.Println(min(celsius(3), 2.5, celsius(-1)), max(celsius(3), 7))
	fmt.Println(min(1.0, 2.0), max(-0.0, 0.0))
	s, t, u := "pear", "apple", "fig"
	fmt.Println(min(s, t), max(s, t), min(s, t, u), max(u, s, t), min(s), max("", s), min("", s))
	fmt.Println(min(name("b"), name("a"), "c"), max(name("z"), "zz"), min("abc", "ab"), max("ab", "abc", "ab\xff"))
	ws := []string{"x", "é", "a", "\x00"}
	m := ws[0]
	for _, w := range ws {
		m = min(m, w)
	}
	fmt.Printf("%q %q\n", m, max(ws[1], ws[2], ws[3]))
	i, j := 3, -7
	fmt.Println(min(i, j), max(i, j, 10), min(uint8(200), 7))
}
