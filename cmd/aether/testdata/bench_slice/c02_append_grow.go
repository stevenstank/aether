package main

// append that grows: 8 rounds of appending 1,000,000 ints to a nil slice.

func main() {
	t := 0
	for r := 0; r < 8; r++ {
		var s []int
		for i := 0; i < 1000000; i++ {
			s = append(s, i^r)
		}
		t += len(s) + s[len(s)/2]
	}
	println(t)
}
