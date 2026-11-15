package main

// Small and zero-size objects: new(int8) takes 8 bytes of Aether's heap,
// its alignment rounding, and new(E) of a zero-size E none (aether.zerobase,
// as gc's zerobase).

type E struct{}

const n = 2_000_000

func main() {
	s := 0
	var last *int8
	for i := 0; i < n; i++ {
		b := new(int8)
		*b = int8(i)
		s += int(*b)
		last = b
	}
	var e *E
	zero := 0
	for i := 0; i < n; i++ {
		e = new(E)
		if e != nil {
			zero++
		}
	}
	println(s, *last, zero)
}
