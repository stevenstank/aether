package main

// Allocation: 300,000 slices of 16 ints from make, each written and read.

func main() {
	t := 0
	for i := 0; i < 300000; i++ {
		s := make([]int, 16)
		s[i%16] = i
		s[15-i%16] += 1
		t += s[i%16] + s[(i+1)%16]
	}
	println(t)
}
