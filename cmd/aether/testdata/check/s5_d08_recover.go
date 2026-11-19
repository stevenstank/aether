// aether:expect run

package main

// recover stops a panic only in the function a deferred call invokes
// directly, while the panic runs it, once; the function that deferred it
// then returns normally, with the results its return stored or zero, and
// the panics raised since it was entered are dropped (D-043).

type P struct{ a, b int }

func helper() any { return recover() }

func superseded() int {
	defer func() { println("superseded recovered", recover().(string)) }()
	defer func() { panic("B") }()
	panic("A")
}

func direct() (int, string) {
	defer func() {
		println("helper sees", helper() == nil)
		println("direct", recover().(string))
	}()
	panic("C")
}

func quiet() {
	defer func() { println("normal return sees", recover() == nil) }()
}

func outer() {
	defer func() {
		quiet()
		println("outer", recover().(string))
	}()
	panic("D")
}

func twice() {
	defer func() { println("second", recover() == nil) }()
	defer func() { println("first", recover().(string)) }()
	panic("E")
}

func kept() int {
	defer func() { println("kept", recover().(string)) }()
	defer func() { panic("late") }()
	return 42
}

func agg(n int) (P, [2]int) {
	defer func() { println("agg", recover().(int)) }()
	if n > 0 {
		panic(n)
	}
	return P{1, 2}, [2]int{3, 4}
}

func nested() {
	defer func() { println("nested outer", recover().(string)) }()
	defer func() {
		defer func() { println("nested inner", recover().(string)) }()
		panic("inner")
	}()
	panic("outer")
}

func many(n int) int {
	defer func() { recover() }()
	if n%2 == 0 {
		panic(n)
	}
	return n
}

func main() {
	println(superseded())
	println(direct())
	outer()
	twice()
	println(kept())
	p, a := agg(1)
	println(p.a, p.b, a[0], a[1])
	p, a = agg(0)
	println(p.a, p.b, a[0], a[1])
	nested()
	t := 0
	for i := 0; i < 1000; i++ {
		t += many(i)
	}
	println(t, recover() == nil)
}
