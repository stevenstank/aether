// aether:expect run

// Type tests whose instance the type checker would reject but Go does
// not: a type switch case an earlier case of the instance already lists,
// which never matches, and a type assertion or case of a type the
// operand can never hold, which never succeeds. A value case of a
// conversion to T is not constant, so the copy's duplicate is no error.
package main

type I interface{ M() int }

type A int

func (a A) M() int { return int(a) }

type B struct{ n int }

func (b *B) M() int { return b.n }

type N int

func kind[T any](x any) int {
	switch x.(type) {
	case T:
		return 1
	case int:
		return 2
	case []T, []int:
		return 4
	}
	return 0
}

func multi[T any](x any) int {
	switch x.(type) {
	case int8, T:
		return 3
	case int16, int32:
		return 5
	}
	return 0
}

func value[T ~int](x T) int {
	switch x {
	case 1:
		return 1
	case T(1), 2:
		return 2
	}
	return 0
}

func bound[T I](x I) int {
	switch v := x.(type) {
	case T, A:
		return v.M() + 100
	case *B:
		return v.M() + 200
	default:
		return v.M()
	}
}

func is[T any](x I) (int, bool) {
	switch x.(type) {
	case T:
		return 1, true
	}
	v, ok := x.(T)
	_ = v
	return 2, ok
}

func must[T any](x I) (r string) {
	defer func() {
		if e := recover(); e != nil {
			r = e.(error).Error()
		}
	}()
	_ = x.(T)
	return "ok"
}

func main() {
	println(kind[int](1), kind[int](int8(2)), kind[int](3), kind[int8](int8(3)), kind[string]("s"))
	println(multi[int8](int8(1)), multi[int16](int16(2)), multi[int](3), multi[int](int16(4)), multi[uint](uint(5)))
	println(kind[int]([]int{}), kind[bool]([]bool{}), kind[bool]([]int{}), kind[bool](2.5))
	println(value(1), value(2), value(3))
	println(bound[A](A(1)), bound[*B](A(2)), bound[*B](&B{3}), bound[I](A(4)))
	println(is[A](A(5)))
	println(is[N](A(6)))
	println(is[string](nil))
	println(must[A](A(7)))
	println(must[N](A(8)))
	println(must[string](nil))
	println(must[*B](A(9)))
}
