// aether:expect run

// Switches on arrays and structs, compared as == compares them, and case
// values of interface type with a tag that is not, or without a tag,
// compared with the tag converted to the case value's type. The tag is
// evaluated once, and the case values in order until one matches.
package main

import "math"

type P struct {
	x, y int
	s    string
}

type W struct{ v any }

type Big [20]int

type Fooer interface{ Foo() int }

type N int

func (n N) Foo() int { return int(n) }

var p = P{1, 2, "a"}

func next(v P, msg string) P {
	println("case", msg)
	p = P{9, 9, "z"}
	return v
}

func point() string {
	switch p {
	case next(P{1, 2, "b"}, "first"):
		return "first"
	case next(P{1, 2, "a"}, "second"):
		return "second"
	case next(P{9, 9, "z"}, "third"):
		return "third"
	}
	return "none"
}

func big(i int) string {
	var b Big
	b[i] = 1
	switch b {
	case Big{}:
		return "zero"
	case Big{19: 1}:
		return "last"
	}
	return "other"
}

func floats(f float64) string {
	switch [2]float64{f, 1} {
	case [2]float64{0, 1}:
		return "zero"
	case [2]float64{f, 1}:
		return "same"
	}
	return "differ"
}

func cplx(c complex128) int {
	switch c {
	case 1 + 2i:
		return 1
	case complex(0, 1):
		return 2
	}
	return 0
}

func uncomparable() (r string) {
	defer func() {
		if e := recover(); e != nil {
			r = e.(error).Error()
		}
	}()
	switch (W{[]int{1}}) {
	case W{1}:
		return "int"
	case W{[]int{1}}:
		return "slice"
	}
	return "none"
}

func mixed(n int, vs ...any) int {
	switch n {
	case vs[0]:
		return 0
	case vs[1], vs[2]:
		return 1
	}
	return -1
}

func ptr(q *int) string {
	switch q {
	case any(nil):
		return "any(nil)"
	case nil:
		return "nil"
	case any(q):
		return "self"
	}
	return "none"
}

func tagless(v any) string {
	switch {
	case v:
		return "true"
	case false:
		return "false"
	}
	return "neither"
}

func fooer(n N, f, g Fooer) string {
	switch n {
	case f:
		return "f"
	case g:
		return "g"
	}
	return "none"
}

func main() {
	println(point(), p.x, p.s)
	println(big(0), big(19), big(5))
	println(floats(0), floats(2), floats(math.NaN()))
	println(cplx(1+2i), cplx(1i), cplx(2))
	println(uncomparable())
	println(mixed(1, 1, 2, 3), mixed(3, int64(3), 2, 3), mixed(2, "2", N(2), 2), mixed(5, nil, []int{}, 5.0))
	x := 1
	println(ptr(nil), ptr(&x))
	println(tagless(true), tagless(false), tagless(1), tagless(nil))
	println(fooer(3, N(1), N(3)), fooer(1, N(1), nil), fooer(2, nil, nil))
	switch s := [2]string{"a", "b"}; s {
	case [2]string{"a", "b"}:
		println("strings")
	}
}
