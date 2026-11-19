// aether:expect run

package main

// A function that recovers returns the values of its named results: those
// set before the panic, or by the deferred call that recovers (D-044).

type P struct{ a [2]int }

func safeDiv(a, b int) (q int, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = e.(error)
			q = -1
		}
	}()
	q = 7
	return a / b, nil
}

func kept() (p P, s string) {
	defer func() { recover() }()
	p.a[0] = 5
	s = "before"
	panic("x")
}

func message() (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = "recovered: " + r.(string)
		}
	}()
	panic("boom")
}

func index(xs []int, i int) (v int, ok bool) {
	defer func() {
		if recover() != nil {
			v, ok = -1, false
		}
	}()
	return xs[i], true
}

func nested() (n int) {
	defer func() {
		recover()
		n *= 10
	}()
	n = inner()
	return n + 1
}

func inner() (m int) {
	defer func() { m = 3 }()
	panic("inner")
}

func main() {
	q, err := safeDiv(6, 3)
	println(q, err == nil)
	q, err = safeDiv(1, 0)
	println(q, err.Error())
	p, s := kept()
	println(p.a[0], s)
	println(message())
	xs := []int{4, 5}
	println(index(xs, 1))
	println(index(xs, 2))
	println(nested())
}
