package main

// Deep recursion: Ackermann's function, whose call depth reaches
// about 2**(n+3).

func ack(m, n int) int {
	if m == 0 {
		return n + 1
	}
	if n == 0 {
		return ack(m-1, 1)
	}
	return ack(m-1, ack(m, n-1))
}

func main() {
	println(ack(2, 3000), ack(3, 10))
}
