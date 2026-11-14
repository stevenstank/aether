package main

// Small structs copied by assignment, passed, and returned: two and four
// words, in a loop the copies dominate.

type P struct {
	x, y int
}

type Q struct {
	a, b int64
	c    int32
	d    bool
}

func add(p, q P) P {
	return P{p.x + q.x, p.y ^ q.y}
}

func step(q Q, i int64) Q {
	q.a += i
	q.b ^= q.a
	q.c++
	q.d = !q.d
	return q
}

func main() {
	p := P{1, 2}
	d := P{3, 5}
	var q Q
	for i := 0; i < 40000000; i++ {
		t := p
		t.x += i
		p = add(t, d)
		r := q
		q = step(r, int64(i))
	}
	println(p.x, p.y, q.a, q.b, q.c, q.d)
}
