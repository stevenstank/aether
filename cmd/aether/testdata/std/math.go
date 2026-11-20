package main

import "math"

func main() {
	xs := []float64{0, -0.5, 2.5, -7.25, 1e300, math.Inf(1), math.NaN(), math.Pi, math.SmallestNonzeroFloat64}
	for _, x := range xs {
		println(math.Floor(x), math.Ceil(x), math.Trunc(x), math.Round(x), math.RoundToEven(x), math.Abs(x))
		println(math.Sqrt(x), math.Float64bits(x), math.Signbit(x), math.IsNaN(x), math.IsInf(x, 0))
		f, e := math.Frexp(x)
		i, frac := math.Modf(x)
		println(f, e, i, frac, math.Max(x, 1), math.Min(x, 1), math.Hypot(x, 3), math.Mod(x, 3))
		println(math.Sin(x), math.Cos(x), math.Atan(x), math.Cbrt(x), math.Float32bits(float32(x)))
	}
	println(math.Float64frombits(0x400921fb54442d18), math.Ldexp(0.5, 10), math.Copysign(3, -1), math.MaxInt64, math.Nextafter(1, 2))
}
