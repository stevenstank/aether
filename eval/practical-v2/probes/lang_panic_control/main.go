package main

import (
	"fmt"
)

type escape struct{ v int }

func find(xs []int) (res int) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*escape); ok {
				res = e.v
				return
			}
			panic(r)
		}
	}()
	for _, x := range xs {
		if x%2 == 0 {
			panic(&escape{x})
		}
	}
	return -1
}

func main() {
	fmt.Println(find([]int{1, 4, 5}))
	defer func() {
		r := recover()
		e, ok := r.(*escape)
		fmt.Println(ok, e.v)
	}()
	panic(&escape{v: 7})
}
