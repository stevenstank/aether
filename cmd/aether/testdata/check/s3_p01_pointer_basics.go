// aether:expect run

package main

// &x of locals and parameters, *p reads and writes, new, and pointers as
// parameters and results: aliases see each other's writes, and each
// execution of a declaration whose address is taken makes a new variable.

func inc(p *int) {
	*p = *p + 1
}

func swap(a, b *int) {
	*a, *b = *b, *a
}

// escape returns the address of its parameter, a copy of the argument.
func escape(x int) *int {
	x++
	return &x
}

func fresh(n int) *int16 {
	v := int16(n)
	return &v
}

func main() {
	x := 1
	p := &x
	q := p
	inc(p)
	*q += 10
	println(x, *p, *q, p == q, p == &x)
	y := 20
	swap(&x, &y)
	println(x, y)
	e := escape(x)
	println(*e, x)
	a, b := fresh(7), fresh(7)
	*b = -3
	println(*a, *b, a == b, a != nil)
	n := new(int)
	pp := &n
	**pp = 5
	*pp = &y
	*n *= 2
	println(*n, **pp, y)
	f := new(float64)
	*f = 1.5
	*f /= 4
	println(*f)
	var s string = "pointer"
	ps := &s
	println(*ps, len(*ps), (*ps)[1])
	b2 := true
	pb := &b2
	*pb = !*pb
	println(b2)
	u := uint8(250)
	pu := &u
	for i := 0; i < 10; i++ {
		*pu++
	}
	println(u)
}
