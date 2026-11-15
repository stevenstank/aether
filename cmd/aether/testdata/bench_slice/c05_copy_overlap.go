package main

// Overlapping copy: 400 shifts each way of a 100,000-element slice.

func main() {
	s := make([]int, 100000)
	for i := 0; i < len(s); i++ {
		s[i] = i
	}
	n := 0
	for r := 0; r < 400; r++ {
		n += copy(s[1:], s)
		n += copy(s, s[2:])
	}
	println(n, s[0], s[50000], s[99999])
}
