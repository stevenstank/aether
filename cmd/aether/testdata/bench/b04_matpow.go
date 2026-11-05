package main

// High register pressure: 3x3 matrix products modulo a prime, with the
// 18 entries of two matrices and the products live in one loop.

func main() {
	const m = 1000000007
	a00, a01, a02 := uint64(1), uint64(1), uint64(0)
	a10, a11, a12 := uint64(1), uint64(0), uint64(1)
	a20, a21, a22 := uint64(0), uint64(1), uint64(1)
	r00, r01, r02 := uint64(1), uint64(0), uint64(0)
	r10, r11, r12 := uint64(0), uint64(1), uint64(0)
	r20, r21, r22 := uint64(0), uint64(0), uint64(1)
	for i := 0; i < 20000000; i++ {
		c00 := (r00*a00 + r01*a10 + r02*a20) % m
		c01 := (r00*a01 + r01*a11 + r02*a21) % m
		c02 := (r00*a02 + r01*a12 + r02*a22) % m
		c10 := (r10*a00 + r11*a10 + r12*a20) % m
		c11 := (r10*a01 + r11*a11 + r12*a21) % m
		c12 := (r10*a02 + r11*a12 + r12*a22) % m
		c20 := (r20*a00 + r21*a10 + r22*a20) % m
		c21 := (r20*a01 + r21*a11 + r22*a21) % m
		c22 := (r20*a02 + r21*a12 + r22*a22) % m
		r00, r01, r02 = c00, c01, c02
		r10, r11, r12 = c10, c11, c12
		r20, r21, r22 = c20, c21, c22
		if i%7 == 0 {
			a00, a11, a22 = a11, a22, a00
		}
	}
	println(r00, r01, r02)
	println(r10, r11, r12)
	println(r20, r21, r22)
}
