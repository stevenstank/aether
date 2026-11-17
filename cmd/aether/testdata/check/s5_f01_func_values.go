// aether:expect run

package main

// Func values (D-039): functions of the program as values, in variables,
// package-level variables, parameters, results, struct fields, array and
// slice elements; calls of them with every kind of argument and result;
// comparison with nil; recursion through a func value; and the order of
// evaluation of the function value and the arguments.

type Pair struct {
	a, b int
}

type Ops struct {
	bin  func(int, int) int
	name string
	pair func(Pair) Pair
}

func add(x, y int) int { return x + y }
func sub(x, y int) int { return x - y }
func mul(x, y int) int { return x * y }

func swap(p Pair) Pair { return Pair{p.b, p.a} }

func divmod(x, y int) (int, int) { return x / y, x % y }

func greet(s string, n int8) string {
	r := ""
	for i := int8(0); i < n; i++ {
		r += s
	}
	return r
}

func apply(f func(int, int) int, x, y int) int { return f(x, y) }

func pick(i int) func(int, int) int {
	switch i {
	case 0:
		return add
	case 1:
		return sub
	}
	return mul
}

var fact func(int) int

func factorial(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}

var table = [3]func(int, int) int{add, sub, mul}
var first = table[2](6, 7)

func trace(s string, v int) int {
	println("eval", s)
	return v
}

func which(s string) func(int, int) int {
	println("func", s)
	return add
}

func floats(x float64, y float32) (float64, float32) { return x * 2, y / 2 }

func noop() {}

func main() {
	println(first)
	f := add
	println(f(1, 2), apply(sub, 10, 3), apply(mul, 4, 5), pick(1)(9, 4))
	g := f
	f = mul
	println(f(3, 3), g(3, 3), f != nil, g == nil)

	var h func(int, int) int
	println(h == nil, nil == h)
	h = sub
	println(h != nil, h(1, 2))

	ops := Ops{bin: add, name: "add", pair: swap}
	p := ops.pair(Pair{1, 2})
	println(ops.name, ops.bin(5, 6), p.a, p.b)
	op := &ops
	op.bin = sub
	println(op.bin(5, 6), ops.bin(5, 6))

	fs := []func(int, int) int{add, sub, mul, nil}
	for i := 0; i < len(fs); i++ {
		if fs[i] == nil {
			println(i, "nil")
			continue
		}
		println(i, fs[i](12, 4), table[i](12, 4))
	}

	dm := divmod
	q, r := dm(17, 5)
	println(q, r)
	println(dm(23, 7))

	gr := greet
	println(gr("ab", 3), len(gr("x", 0)))

	fl := floats
	println(fl(1.5, 3))

	fact = factorial
	println(fact(10))

	n := noop
	n()

	// The function value, then the arguments, left to right.
	println(which("a")(trace("x", 1), trace("y", 2)))
	fs[0] = mul
	i := 0
	println(fs[i](trace("i", 3), trace("j", 4)))
}
