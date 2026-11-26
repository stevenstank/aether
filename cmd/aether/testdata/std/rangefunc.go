package main

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
)

func count(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := range n {
			if !yield(i) {
				return
			}
		}
	}
}

func pairs(s string) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		for i, w := range strings.Fields(s) {
			if !yield(i, w) {
				return
			}
		}
	}
}

func find(s string, w string) (int, bool) {
	for i, x := range pairs(s) {
		if x == w {
			return i, true
		}
	}
	return -1, false
}

func named(limit int) (sum int, err error) {
	for i := range count(100) {
		if i > limit {
			err = fmt.Errorf("over %d", limit)
			return
		}
		sum += i
	}
	return sum, nil
}

func nested() []string {
	var out []string
outer:
	for i := range count(4) {
		for j := range count(4) {
			if j > i {
				continue outer
			}
			if i == 3 {
				break outer
			}
			out = append(out, fmt.Sprint(i, j))
		}
	}
	return out
}

func bad(yield func(int) bool) {
	yield(1)
	yield(2)
}

func main() {
	for i := range count(5) {
		if i == 1 {
			continue
		}
		if i == 4 {
			break
		}
		fmt.Print(i, " ")
	}
	fmt.Println()
	for range count(2) {
		fmt.Print("x")
	}
	fmt.Println()
	var k int
	var v string
	for k, v = range pairs("a b c") {
	}
	fmt.Println(k, v)
	fmt.Println(find("alpha beta gamma", "gamma"))
	fmt.Println(find("alpha", "zeta"))
	fmt.Println(named(10))
	fmt.Println(named(1000))
	fmt.Println(nested())
	m := map[string]int{"b": 2, "a": 1, "c": 3}
	fmt.Println(slices.Sorted(maps.Keys(m)), slices.Collect(maps.Values(map[int]int{1: 7})))
	for i, x := range slices.All([]string{"p", "q"}) {
		fmt.Print(i, x, ";")
	}
	for i, x := range slices.Backward([]int{1, 2, 3}) {
		fmt.Print(i, ":", x, " ")
	}
	fmt.Println()
	fns := []func() int{}
	for i := range count(3) {
		fns = append(fns, func() int { return i * 10 })
	}
	for _, f := range fns {
		fmt.Print(f(), " ")
	}
	fmt.Println()
	defer func() { fmt.Println("recovered:", recover()) }()
	for x := range bad {
		fmt.Println("got", x)
		break
	}
}
