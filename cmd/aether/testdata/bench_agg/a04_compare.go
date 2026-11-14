package main

// == and != on arrays and structs: equal values, which compare every
// element, and values differing early and late.

type Key struct {
	a, b int32
	c    float64
	d    [6]uint8
}

func main() {
	var x, y [256]int32
	for i := 0; i < 256; i++ {
		x[i] = int32(i)
		y[i] = int32(i)
	}
	k1 := Key{1, 2, 0.5, [6]uint8{1, 2, 3, 4, 5, 6}}
	k2 := k1
	eq, ne, keq := 0, 0, 0
	for i := 0; i < 800000; i++ {
		j := i % 256
		y[j]++
		if x == y {
			eq++
		}
		y[j]--
		if x == y {
			eq++
		}
		k2.d[i%6] ^= 1
		if k1 == k2 {
			keq++
		}
		if k1 != k2 {
			ne++
		}
	}
	println(eq, ne, keq)
}
