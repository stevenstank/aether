package main

// A small leaf function called from a doubly nested loop.

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func main() {
	sum, coprime := 0, 0
	for i := 1; i <= 4000; i++ {
		for j := 1; j <= 4000; j++ {
			g := gcd(i, j)
			sum += g
			if g == 1 {
				coprime++
			}
		}
	}
	println(sum, coprime)
}
