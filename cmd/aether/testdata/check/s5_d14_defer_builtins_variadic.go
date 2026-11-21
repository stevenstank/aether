// aether:expect run

// Deferred close and delete, and deferred and go calls of variadic
// functions: the arguments, and the slice of a variadic call's elements,
// are evaluated at the statement; a deferred close or delete that panics
// panics where it runs.
package main

type K struct {
	a int
	s string
}

func sum(prefix string, xs ...int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	println(prefix, len(xs), t)
	return t
}

type big struct{ a [4]int }

func mk(xs ...int) big {
	var b big
	copy(b.a[:], xs)
	println("mk", b.a[0], b.a[1], b.a[2])
	return b
}

func send(done chan int, xs ...int) { done <- len(xs) }

func work(m map[K]int, ch chan int) (n int) {
	defer close(ch)
	k := K{1, "x"}
	defer delete(m, k)
	k.a = 2
	n = 5
	xs := []int{1, 2, 3}
	defer sum("spread", xs...)
	xs[0] = 100
	defer sum("packed", n, n*2)
	defer sum("none")
	defer mk(7, 8)
	ch <- 1
	return n
}

func main() {
	m := map[K]int{{1, "x"}: 1, {2, "x"}: 2}
	ch := make(chan int, 1)
	println("work", work(m, ch), len(m), m[K{2, "x"}])
	_, ok := <-ch
	v, ok2 := <-ch
	println(ok, v, ok2)
	done := make(chan int)
	go send(done, 1, 2, 3)
	println("go", <-done)
	var nilm map[string]int
	func() {
		defer func() { println("recovered:", recover() != nil) }()
		var c chan int
		defer delete(nilm, "a")
		defer close(c)
	}()
	func() {
		defer func() {
			r := recover()
			println("recovered:", r.(error).Error())
		}()
		c := make(chan int)
		close(c)
		defer close(c)
	}()
	func() {
		defer func() {
			r := recover()
			println("recovered:", r.(error).Error())
		}()
		m := map[any]int{1: 1}
		var k any = []int{1}
		defer delete(m, k)
		println("body", len(m))
	}()
}
