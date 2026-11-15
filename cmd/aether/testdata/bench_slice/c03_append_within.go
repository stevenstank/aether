package main

// append within the capacity: one array of 1,000,000 ints refilled 40 times
// through s[:0].

func main() {
	s := make([]int, 0, 1000000)
	t := 0
	for r := 0; r < 40; r++ {
		s = s[:0]
		for i := 0; i < 1000000; i++ {
			s = append(s, i+r)
		}
		t += s[len(s)-1]
	}
	println(t, len(s))
}
