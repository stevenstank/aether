// aether:expect run

package main

// Regressions found by the GOROOT conformance sweep (D-049, 11.1): a switch
// whose tag is a slice, func, or map compared with nil
// (test/fixedbugs/issue53635.go), and the address of a slice or map
// literal (test/fixedbugs/bug119.go).

func tags(s []int, f func() int, m map[int]int) {
	switch s {
	case nil:
		println("nil slice")
	default:
		println("slice", len(s))
	}
	switch f {
	case nil:
		println("nil func")
	default:
		println("func", f())
	}
	switch m {
	case nil:
		println("nil map")
	default:
		println("map", len(m))
	}
}

func first(a []int) int { return a[0] }

func main() {
	tags(nil, nil, nil)
	tags([]int{}, func() int { return 7 }, map[int]int{1: 2})
	a := &[]int{12, 13}
	if x := (*a)[0]; x != 12 {
		panic(2)
	}
	println(first(*a), len(*a), cap(*a))
	*a = append(*a, 14)
	println((*a)[2], len(*a))
	m := &map[string]int{"x": 1}
	(*m)["y"] = 2
	println(len(*m), (*m)["x"], (*m)["y"])
}
