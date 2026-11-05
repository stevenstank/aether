package main

// Mixing arithmetic on four uint64 lanes with rotations, in the style of
// a non-cryptographic hash.

func rotl(x uint64, r uint) uint64 { return x<<r | x>>(64-r) }

func main() {
	const p1 = 11400714785074694791
	const p2 = 14029467366897019727
	var q1, q2 uint64 = p1, p2
	v1, v2, v3, v4 := q1+q2, q2, uint64(0), 0-q1
	x := uint64(88172645463325252)
	for i := 0; i < 10000000; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		v1 = rotl(v1+x*p2, 31) * p1
		v2 = rotl(v2+(x>>1)*p2, 31) * p1
		v3 = rotl(v3+(x^v1)*p2, 31) * p1
		v4 = rotl(v4+(x+v2)*p2, 31) * p1
	}
	h := rotl(v1, 1) + rotl(v2, 7) + rotl(v3, 12) + rotl(v4, 18)
	h ^= h >> 33
	h *= p2
	h ^= h >> 29
	println(h)
}
