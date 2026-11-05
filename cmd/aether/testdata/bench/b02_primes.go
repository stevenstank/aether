package main

// Division-dominated: count primes by trial division.

func isPrime(n int) bool {
	if n < 2 {
		return false
	}
	if n%2 == 0 {
		return n == 2
	}
	for d := 3; d*d <= n; d += 2 {
		if n%d == 0 {
			return false
		}
	}
	return true
}

func main() {
	count, last := 0, 0
	for n := 0; n < 1500000; n++ {
		if isPrime(n) {
			count++
			last = n
		}
	}
	println(count, last)
}
