// aether:expect run

// Variadic methods, interface methods, func values, and function
// literals, called with arguments, with a slice and ..., or with the
// results of a call, directly and in defer and go statements, and as
// statements.
package main

type U struct{ base int }

func (u U) Sum(xs ...int) int {
	s := u.base
	for _, x := range xs {
		s += x
	}
	return s
}

func (u *U) Add(xs ...int) {
	for _, x := range xs {
		u.base += x
	}
}

func (u *U) AddThen(done chan bool, xs ...int) {
	u.Add(xs...)
	done <- true
}

type Summer interface{ Sum(...int) int }

type Box[T any] struct{ items []T }

func (b *Box[T]) Put(vs ...T) int {
	b.items = append(b.items, vs...)
	return len(b.items)
}

func sum(xs ...int) int { return U{}.Sum(xs...) }

func two() (int, int) { println("two"); return 3, 4 }

func three() (string, int, int) { return "x", 5, 6 }

func one() (string, int) { return "only", 0 }

func describe(name string, vs ...any) {
	print(name, ":", len(vs), vs == nil)
	for _, v := range vs {
		switch v := v.(type) {
		case int:
			print(" int ", v)
		case string:
			print(" string ", v)
		}
	}
	println()
}

func clear0(xs ...int) {
	if len(xs) > 0 {
		xs[0] = 0
	}
}

func main() {
	u := U{10}
	println(u.Sum(), u.Sum(1), u.Sum(1, 2, 3), u.Sum([]int{4, 5}...), u.Sum(two()))
	u.Add(1, 2)
	u.Add()
	println(u.base)
	var i Summer = u
	println(i.Sum(), i.Sum(7, 8), i.Sum(nil...))
	i = &u
	println(i.Sum(1))

	m := u.Sum
	me := U.Sum
	pe := (*U).Add
	pe(&u, 100)
	println(m(1), me(U{1}, 2, 3), u.base)

	var f func(...int) int = sum
	println(f(), f(1, 2), f(two()), f([]int{9}...))
	g := func(sep string, xs ...string) string {
		r := ""
		for k, x := range xs {
			if k > 0 {
				r += sep
			}
			r += x
		}
		return r
	}
	println(g("-"), g("-", "a"), g("-", "a", "b", "c"))

	describe(three())
	describe(one())
	describe("none")
	describe("list", 1, "two", 3)

	s := []int{1, 2}
	clear0(s...)
	t := []int{1, 2}
	clear0(t[0], t[1])
	println(s[0], t[0])

	var b Box[string]
	println(b.Put("a", "b"), b.Put(), b.Put([]string{"c"}...))

	done := make(chan bool)
	go func(xs ...int) {
		println("go", len(xs), sum(xs...))
		done <- true
	}(1, 2, 3)
	<-done
	go (&u).AddThen(done, 1000)
	<-done
	go f(two())
	defer func() {
		println("recovered:", recover() != nil)
		println("deferred", u.base)
	}()
	defer u.Add(5, 5)
	defer describe(three())
	defer i.Sum(1, 2)
	var nilf func(...int) int
	defer nilf(1)
}
