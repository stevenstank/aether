package main

import (
	"fmt"
	"sync"
)

type counter struct{ n int }

func (c *counter) inc() { c.n++ }

func main() {
	var fs []func() int
	for i := 0; i < 3; i++ {
		fs = append(fs, func() int { return i })
	}
	for _, f := range fs {
		fmt.Print(f(), " ")
	}
	fmt.Println()

	var ps []*int
	for i := 0; i < 3; i++ {
		ps = append(ps, &i)
		i++ // the post statement sees this iteration's change
	}
	for _, p := range ps {
		fmt.Print(*p, " ")
	}
	fmt.Println()

	// A closure that changes the variable: the next iteration starts from it.
	for i := 0; i < 10; i++ {
		bump := func() { i += 3 }
		bump()
		fmt.Print(i, " ")
	}
	fmt.Println()

	var wg sync.WaitGroup
	out := make([]int, 5)
	for c := 0; c < 5; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[c] = c * c
		}()
	}
	wg.Wait()
	fmt.Println(out)

	var cs []*counter
	for c := (counter{}); c.n < 3; c.inc() {
		cs = append(cs, &c)
	}
	for _, c := range cs {
		fmt.Print(c.n, " ")
	}
	fmt.Println()

	var arrs []func() [2]int
	for a, k := [2]int{1, 2}, 0; k < 2; a[0], k = a[0]*10, k+1 {
		arrs = append(arrs, func() [2]int { return a })
	}
	fmt.Println(arrs[0](), arrs[1]())

	var gs []func() string
	for s := "a"; len(s) < 4; s += "b" {
		gs = append(gs, func() string { return s })
	}
	for _, g := range gs {
		fmt.Print(g(), " ")
	}
	fmt.Println()

	var hs []func() int
	for i, j := 0, 10; i < j; i, j = i+1, j-2 {
		hs = append(hs, func() int { return i*100 + j })
	}
	for _, h := range hs {
		fmt.Print(h(), " ")
	}
	fmt.Println()
	n := 0
	for i := 0; ; {
		defer func() { fmt.Println("deferred sees", i) }()
		i++
		n++
		if n == 2 {
			break
		}
	}
}
