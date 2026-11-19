// aether:expect run

package main

// Deferred calls of functions, methods with value and pointer receivers,
// interface methods, func values, closures, print, and println, run last
// in first out when the function returns; operands are evaluated at the
// defer statement, captured variables when the call runs (D-043).

type P struct{ x, y int }

func (p P) show(tag string) { println(tag, p.x, p.y) }

func (p *P) bump(n int) { p.x += n; println("bump", p.x) }

type Shower interface{ show(tag string) }

func pair() (int, string) { return 7, "seven" }

func two(n int, s string) { println("two", n, s) }

func sum(a [3]int) int {
	s := 0
	for i := 0; i < 3; i++ {
		s += a[i]
	}
	println("sum", s)
	return s
}

func mk(n int) P { println("mk", n); return P{n, -n} }

func get(n int) int { println("get", n); return n }

func loop(n int) int {
	for i := 0; i < n; i++ {
		defer println("loop", i)
	}
	return n * 10
}

func counter() int {
	c := 0
	defer func() { c++; println("closure sees", c) }()
	c = 41
	return c
}

func nested() {
	defer println("nested outer")
	func() {
		defer println("literal")
		println("in literal")
	}()
	println("after literal")
}

func main() {
	p := P{1, 2}
	defer p.show("value receiver")
	defer p.bump(get(5))
	p.x = 100
	var s Shower = P{3, 4}
	defer s.show("interface")
	f := two
	defer f(pair())
	f = nil
	a := [3]int{1, 2, 3}
	defer sum(a)
	a[0] = 50
	defer mk(get(9)).show("result")
	defer print("print", 1, true, "\n")
	defer println(pair())
	println(loop(3))
	println(counter())
	nested()
	println("main body done", p.x)
}
