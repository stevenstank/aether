// explog prints math.Exp, Log, and functions built on them for edge cases,
// and a hash of their results' bits for n generated inputs per family
// (os.Args[1], default 100000): random bit patterns, Exp's argument range,
// Exp's round-half exponent boundaries and their neighbors, subnormals,
// Log near 1 and near Sqrt2/2, and Pow (D-053).
package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
)

var state uint64 = 0x9E3779B97F4A7C15

func next() uint64 {
	state ^= state << 13
	state ^= state >> 7
	state ^= state << 17
	return state
}

func unit() float64 { return float64(next()>>11) / (1 << 53) }

func ulps(x float64, d int64) float64 {
	return math.Float64frombits(uint64(int64(math.Float64bits(x)) + d))
}

const fnvPrime = 1099511628211

type hash uint64

func (h *hash) add(f float64) {
	*h = (*h ^ hash(math.Float64bits(f))) * fnvPrime
}

func main() {
	n := 100000
	if len(os.Args) > 1 {
		n, _ = strconv.Atoi(os.Args[1])
	}
	edges := []float64{0, math.Copysign(0, -1), 1, -1, 0.5, 2, math.E, 10, 100, -100,
		709.782712893384, 709.78271289338397, 709.78271289338399, 709.79, 710, 1e10, 1e308, math.MaxFloat64,
		-708.39, -708.4, -709, -720, -744.44, -745.13321910194110842, -745.1332191019412, -745.2, -746, -1000, -1e10, -1e300, -math.MaxFloat64,
		5e-324, -5e-324, 1e-310, 2.2250738585072009e-308, 2.2250738585072014e-308, 1e-300, -1e-300, 0x1p-30,
		math.Inf(1), math.Inf(-1), math.NaN(), -math.NaN(), math.Float64frombits(0x7FF0000000000001),
		math.Float64frombits(0xFFF8000000000005), math.Sqrt2 / 2, ulps(math.Sqrt2/2, 1), ulps(math.Sqrt2/2, -1),
		ulps(1, 1), ulps(1, -1), 0.6931471805599453, 1.0397207708399179}
	for _, x := range edges {
		fmt.Printf("%016x exp %016x log %016x\n", math.Float64bits(x), math.Float64bits(math.Exp(x)), math.Float64bits(math.Log(x)))
	}
	families := []struct {
		name string
		gen  func(i int) float64
	}{
		{"bits", func(int) float64 { return math.Float64frombits(next()) }},
		{"exprange", func(int) float64 { return -750 + 1460*unit() }},
		{"small", func(int) float64 { return (unit() - 0.5) * math.Ldexp(1, int(next()%60)-50) }},
		// x where LOG2E*x is near n+0.5, where CVTSD2SL rounds half to even.
		{"halves", func(i int) float64 {
			k := float64(int64(next()%2100) - 1075)
			return ulps((k+0.5)/math.Log2E, int64(next()%5)-2)
		}},
		{"subnormal", func(int) float64 { return math.Float64frombits(next() & 0x000FFFFFFFFFFFFF) }},
		{"near1", func(int) float64 { return ulps(1, int64(next()%2000000)-1000000) }},
		{"hsqrt2", func(int) float64 {
			return math.Ldexp(ulps(math.Sqrt2/2, int64(next()%2001)-1000), int(next()%200)-100)
		}},
		{"positive", func(int) float64 { return math.Float64frombits(next() &^ (1 << 63)) }},
	}
	for _, f := range families {
		var he, hl, hp hash
		for i := 0; i < n; i++ {
			x := f.gen(i)
			he.add(math.Exp(x))
			hl.add(math.Log(x))
			if i%16 == 0 {
				hp.add(math.Pow(math.Abs(x), 0.37+unit()))
			}
		}
		fmt.Printf("%s exp %016x log %016x pow %016x\n", f.name, uint64(he), uint64(hl), uint64(hp))
	}
	fmt.Println(math.Pow(2.5, 3.7), math.Pow(10, -2.5), math.Log10(1234.5), math.Log2(7), math.Log1p(1e-5),
		math.Sinh(1.5), math.Cosh(-2), math.Tanh(0.3), math.Asinh(3), math.Acosh(3), math.Atanh(0.5))
}
