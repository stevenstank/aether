// aether:expect run

package main

// Named results are variables of the function, zero at entry: a bare
// return returns their values, a return with values returns those; they
// may be arrays, structs, strings, interfaces, blank, address-taken, or
// captured by a function literal, in functions, methods, and literals
// (D-044).

type P struct {
	a [2]int
	s string
}

func zero() (n int, s string, ok bool, p P, e error) { return }

func bare(k int) (x, _ int, y string) {
	x = k * 3
	y = "y"
	if k > 1 {
		return
	}
	x++
	return
}

func swap() (a, b int) {
	a, b = 1, 2
	return b, a
}

func valuesWin() (r [3]int) {
	r[0] = 9
	return [3]int{1, 2, 3}
}

func pt(k int) (p P) {
	p.a[1] = k
	p.s = "p"
	if k < 0 {
		return P{s: "neg"}
	}
	return
}

func addr() (r int, q *int) {
	q = &r
	*q = 5
	return
}

func counter() (n int, inc func() int) {
	inc = func() int { n++; return n }
	inc()
	inc()
	return
}

func (p P) twice() (q P) {
	q = p
	q.a[0] *= 2
	return
}

func (p *P) total() (sum int) {
	for i := 0; i < len(p.a); i++ {
		sum += p.a[i]
	}
	return
}

func pair() (int, string) { return 4, "four" }

func fromCall() (n int, s string) { return pair() }

func main() {
	n, s, ok, p, e := zero()
	println(n, s == "", ok, p.a[0], p.a[1], p.s == "", e == nil)
	println(bare(1))
	println(bare(2))
	println(swap())
	println(valuesWin()[0])
	q := pt(3)
	println(q.a[1], q.s, pt(-1).s, pt(-1).a[1])
	r, rp := addr()
	*rp = 6
	println(r, *rp)
	c, inc := counter()
	println(c, inc(), inc())
	t := P{a: [2]int{3, 4}}
	println(t.twice().a[0], t.a[0], (&t).total())
	println(fromCall())
	lit := func(k int) (out string, m map[string]int) {
		m = map[string]int{"k": k}
		out = "lit"
		return
	}
	o, m := lit(8)
	println(o, m["k"])
}
