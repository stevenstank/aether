// aether:expect run

package main

// Generic functions (D-045): explicit and inferred instantiation,
// constraints with methods and with type sets, instances calling
// instances, recursion, closures, func values of instances, and constant
// conversions to a type parameter, which are not constants: T(100) * T(3)
// wraps for int8, and T(0.1) + T(0.2) rounds twice.

type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint8 | ~uint
}

type Float interface{ ~float32 | ~float64 }

type Number interface {
	Integer | Float
}

type Ordered interface {
	Integer | Float | ~string
}

func Sum[T Number](xs []T) T {
	var s T
	for i := 0; i < len(xs); i++ {
		s += xs[i]
	}
	return s
}

func Max[T Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func MaxOf[T Ordered](xs []T) T {
	m := xs[0]
	for i := 1; i < len(xs); i++ {
		m = Max(m, xs[i])
	}
	return m
}

func Map[T, U any](xs []T, f func(T) U) []U {
	r := make([]U, 0, len(xs))
	for i := 0; i < len(xs); i++ {
		r = append(r, f(xs[i]))
	}
	return r
}

func Filter[T any](xs []T, keep func(T) bool) []T {
	var r []T
	for i := 0; i < len(xs); i++ {
		if keep(xs[i]) {
			r = append(r, xs[i])
		}
	}
	return r
}

func Reduce[T, A any](xs []T, a A, f func(A, T) A) A {
	for i := 0; i < len(xs); i++ {
		a = f(a, xs[i])
	}
	return a
}

func Index[T comparable](xs []T, v T) int {
	for i := 0; i < len(xs); i++ {
		if xs[i] == v {
			return i
		}
	}
	return -1
}

func Wrap[T Integer](x T) T { return x*T(100)*T(3) + T(7) }

func Tenths[T Float](x T) T { return x*T(0.1) + T(0.2) }

func Pow[T Number](x T, n int) T {
	if n == 0 {
		return T(1)
	}
	return x * Pow(x, n-1)
}

func Counter[T Integer](step T) func() T {
	var n T
	return func() T {
		n += step
		return n
	}
}

func Swap[A, B any](a A, b B) (B, A) { return b, a }

func Zero[T any]() (z T) { return }

type P struct{ x, y int }

func (p P) Less(q P) bool { return p.x < q.x || p.x == q.x && p.y < q.y }

type Lesser[T any] interface{ Less(T) bool }

func Min[T Lesser[T]](xs []T) T {
	m := xs[0]
	for i := 1; i < len(xs); i++ {
		if xs[i].Less(m) {
			m = xs[i]
		}
	}
	return m
}

func main() {
	println(Sum([]int{1, 2, 3}), Sum([]float64{0.5, 0.25}), Sum([]int8{100, 100}), Sum[uint8]([]uint8{200, 100}))
	println(Max(3, 7), Max("b", "a"), Max[float64](1, 2.5), MaxOf([]string{"pear", "apple", "zoo", "fig"}))
	strs := Map([]int{1, 22, 333}, func(i int) string { return string(rune('a' + i%26)) })
	println(len(strs), strs[0], strs[1], strs[2])
	evens := Filter([]int{1, 2, 3, 4, 5, 6}, func(i int) bool { return i%2 == 0 })
	println(len(evens), evens[0], evens[2])
	println(Reduce([]string{"a", "bc", "def"}, 0, func(n int, s string) int { return n + len(s) }))
	println(Index([]string{"x", "y"}, "y"), Index([]P{{1, 2}}, P{1, 3}), Index([]float64{0.5}, 0.5))
	println(Wrap(int8(1)), Wrap(1), Wrap(uint8(2)), Wrap(int16(1000)))
	println(Tenths(1.0), Tenths(float32(1)), Tenths(3.0))
	println(Pow(2, 10), Pow(int8(3), 5), Pow(1.5, 3))
	c := Counter(int8(50))
	c()
	c()
	println(c(), c())
	b, a := Swap(1, "one")
	println(a, b)
	println(Zero[int](), Zero[string]() == "", Zero[P]().y, Zero[*P]() == nil, Zero[[]int]() == nil)
	println(Min([]P{{3, 1}, {1, 9}, {1, 2}}).y)
	f := Max[int]
	g := Map[int, int]
	h := g([]int{4, 5}, func(i int) int { return f(i, 5) * 2 })
	println(h[0], h[1], len(h))
	var sums []func([]int) int
	sums = append(sums, Sum[int])
	var s2 func([]int) int = Sum
	sums = append(sums, s2)
	println(sums[0]([]int{7}), sums[1]([]int{8, 9}))
}
