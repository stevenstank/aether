package main

// Address-taken locals in loop bodies: in the first loop the address
// outlives the iteration, in the second it never leaves it. Aether puts
// every one on the heap (D-035); gc's escape analysis can keep the second
// on the stack, and collects the first.

const n = 4_000_000

func main() {
	var p *int
	s := 0
	for i := 0; i < n; i++ {
		x := i
		p = &x
		s += *p
	}
	t := 0
	for i := 0; i < n; i++ {
		y := i * 3
		q := &y
		*q += 1
		t += *q
	}
	println(s, t, *p)
}
