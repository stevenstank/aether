package main

// Indexing: 100 passes of reads and writes over 1,000,000 ints.

func main() {
	s := make([]int, 1000000)
	for i := 0; i < len(s); i++ {
		s[i] = i
	}
	t := 0
	for r := 0; r < 100; r++ {
		for i := 1; i < len(s); i++ {
			s[i] += s[i-1] & 7
			t += s[i]
		}
	}
	println(t)
}
