// aether:expect run

package main

// The blank identifier (D-049): in var declarations, local and at package
// level, in := and =, as a select and range target, where each value is
// evaluated, in order, and discarded.

var calls int

func f(tag string) int {
	calls++
	println("f", tag, calls)
	return calls
}

func two() (int, string) { return f("two"), "s" }

var _ = f("package")

const _ = 1

var a, _ = f("a"), f("blank")

var _, b = two()

type P struct{ x, y int }

type I interface{ M() int }

func (p P) M() int { return p.x }

func main() {
	_ = f("assign")
	_, s := two()
	n, _ := two()
	_, _ = f("left"), f("right")
	var _ = f("local")
	var _ int
	const _, d = 1, 2
	var _ I = P{3, 4}
	var _, c = f("c1"), f("c2")
	_ = P{1, 2}
	_ = []int{f("elem")}
	m := map[string]int{"k": 7}
	_, ok := m["k"]
	_ = m["missing"]
	var i any = P{5, 6}
	_, isP := i.(P)
	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3
	_ = <-ch
	_, open := <-ch
	select {
	case _ = <-ch:
		println("select")
	}
	close(ch)
	for _ = range ch {
	}
	for k, _ := range m {
		println(k)
	}
	var k string
	for k, _ = range m {
		println("k", k)
	}
	println(a, b, s, n, c, d, ok, isP, open, calls)
	defer func() {
		r := recover()
		println("recovered", r != nil)
	}()
	var z []int
	_ = z[f("index")]
}
