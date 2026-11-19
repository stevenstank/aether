// aether:expect run

package main

// With named results, a deferred call runs after the return assigned
// them and may read and set them; the function returns their values then
// (D-044).

type P struct{ x, y int }

func double() (r int) {
	defer func() { r *= 2 }()
	return 21
}

func order() (s string) {
	defer func() { s += "c" }()
	defer func() { s += "b" }()
	s = "x"
	return "a"
}

func seen() (n int) {
	defer func() { println("deferred sees", n) }()
	n = 1
	return n + 10
}

func agg() (p P, a [2]string) {
	defer func() {
		p.y = p.x + 1
		a[1] = a[0] + "!"
	}()
	return P{x: 4}, [2]string{"hi"}
}

func bareDefer() (x, y int) {
	defer func() { x, y = y, x }()
	x, y = 1, 2
	return
}

func argsAtDefer() (r int) {
	r = 1
	defer println("argument", r)
	r = 2
	return 3
}

type T struct{ n int }

func (t *T) next() (v int) {
	defer func() { t.n = v }()
	return t.n + 1
}

func swapResults() (a, b int) {
	defer func() {}()
	a, b = 1, 2
	return b, a
}

func viaPointer() (r int) {
	p := &r
	defer func() { *p += 100 }()
	return 1
}

func main() {
	println(double())
	println(order())
	println(seen())
	p, a := agg()
	println(p.x, p.y, a[0], a[1])
	println(bareDefer())
	println(argsAtDefer())
	var t T
	println(t.next(), t.next(), t.n)
	println(swapResults())
	println(viaPointer())
	f := func() (n int) {
		defer func() { n++ }()
		return 1
	}
	println(f())
}
