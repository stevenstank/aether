package main

// A nested struct of 21 scalars, more than the lowering unrolls into one
// copy, copied through variables, an array of them, and a call.

type Inner struct {
	f float64
	g bool
	h [3]int16
}

type Outer struct {
	id    int
	in    Inner
	pairs [4]struct{ a, b int32 }
	tail  [2]Inner
}

func bump(o Outer, k int) Outer {
	o.id += k
	o.in.h[k%3]++
	o.pairs[k%4].a += int32(k)
	o.tail[k%2].f += 0.5
	return o
}

func main() {
	var ring [8]Outer
	var cur Outer
	for i := 0; i < 9000000; i++ {
		cur = bump(cur, i)
		ring[i%8] = cur
		prev := ring[(i+3)%8]
		cur.in.g = prev.in.g != cur.in.g
	}
	s := 0
	for i := 0; i < 8; i++ {
		s += ring[i].id + int(ring[i].in.h[1]) + int(ring[i].pairs[3].a)
	}
	println(s, cur.id, cur.in.g, cur.tail[1].f, cur.in.h[0], cur.in.h[2])
}
