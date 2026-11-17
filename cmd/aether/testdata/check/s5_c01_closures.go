// aether:expect run

package main

// Closures (D-040): function literals that capture variables of every kind
// by reference, share them with the function around them and with each
// other, outlive it, nest, recurse through a captured variable, and are
// stored, passed, returned, and called at once.

type Point struct{ x, y int }

type holder struct{ f func(int) }

func (p *Point) Move(dx int) { p.x += dx }

func counter(start int) (func() int, func(int)) {
	n := start // a captured parameter's copy
	return func() int {
			n++
			return n
		}, func(k int) {
			n = k
		}
}

func adder(base [3]int) func(int) [3]int {
	return func(k int) [3]int {
		for i := 0; i < 3; i++ {
			base[i] += k
		}
		return base
	}
}

func compose(f, g func(int) int) func(int) int {
	return func(x int) int { return f(g(x)) }
}

var twice = func(x int) int { return 2 * x }
var inc = func(x int) int { return x + 1 }
var both = compose(twice, inc)

func apply(n int, f func(int) int) int {
	for i := 0; i < n; i++ {
		f(i)
	}
	return f(-1)
}

func main() {
	next, reset := counter(10)
	println(next(), next())
	reset(100)
	println(next())
	n2, r2 := counter(0)
	r2(-5)
	println(n2(), next())

	a := adder([3]int{1, 2, 3})
	r := a(10)
	println(r[0], r[2], a(1)[1])

	println(both(5), compose(inc, twice)(5), twice(inc(0)))

	// Shared variables, assigned before and after the closure is made.
	x := 1
	s := "s"
	p := Point{1, 2}
	arr := [2]string{"a", "b"}
	sl := []int{1, 2}
	f := func() string {
		x *= 3
		s += "!"
		p.Move(x)
		arr[1] += s
		sl = append(sl, x)
		return s
	}
	x = 2
	println(f(), x, p.x, arr[1], len(sl), sl[2])
	println(f(), x, p.x, arr[1], len(sl))

	// Nested: the inner literal captures through the middle one.
	total := 0
	mk := func(k int) func() int {
		local := k * 10
		return func() int {
			total += local + k
			local++
			return total
		}
	}
	g1, g2 := mk(1), mk(2)
	println(g1(), g2(), g1(), total)

	// Recursion through a captured variable.
	var fact func(int) int
	fact = func(n int) int {
		if n <= 1 {
			return 1
		}
		return n * fact(n-1)
	}
	println(fact(12))

	// Each iteration's body variable is its own.
	var fs []func() int
	for i := 0; i < 3; i++ {
		j := i * i
		fs = append(fs, func() int { j++; return j })
	}
	println(fs[0](), fs[1](), fs[2](), fs[2]())

	sum := 0
	println(apply(4, func(i int) int { sum += i; return sum }), sum)

	// Called at once, with several results.
	q, rem := func(a, b int) (int, int) { return a / b, a % b }(17, 5)
	println(q, rem)

	// A closure in a struct field, using a method value.
	h := holder{p.Move}
	h.f(1000)
	println(p.x)
	pp := &p
	mv := func() { pp.Move(1) }
	mv()
	println(p.x, func() bool { return pp == &p }())
}
