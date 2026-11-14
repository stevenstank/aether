package main

// A 1024-element array copied whole: by assignment and through a call that
// returns it changed.

type Vec [1024]int64

func mix(v Vec, k int) Vec {
	v[k%1024] += int64(k)
	v[(k*7)%1024] ^= v[k%1024]
	return v
}

func main() {
	var a, b Vec
	for i := 0; i < 1024; i++ {
		a[i] = int64(i * i)
	}
	for i := 0; i < 200000; i++ {
		b = a
		b[i%1024]++
		a = mix(b, i)
	}
	var s int64
	for i := 0; i < 1024; i++ {
		s += a[i] ^ int64(i)
	}
	println(s, a[0], a[1023], b[512])
}
