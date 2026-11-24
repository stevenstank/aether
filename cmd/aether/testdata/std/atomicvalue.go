package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type point struct{ x, y int }

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%s: panic: %v\n", name, r)
		}
	}()
	f()
}

func main() {
	// Pointer[T] of types sync/atomic can name.
	var p atomic.Pointer[int]
	fmt.Println(p.Load() == nil)
	a, b := 1, 2
	p.Store(&a)
	fmt.Println(*p.Load(), p.CompareAndSwap(&b, &a), p.CompareAndSwap(&a, &b), *p.Load())
	old := p.Swap(nil)
	fmt.Println(*old, p.Load() == nil, p.CompareAndSwap(nil, &a), *p.Load())
	var ps atomic.Pointer[[]string]
	words := []string{"x", "y"}
	ps.Store(&words)
	*ps.Load() = append(*ps.Load(), "z")
	fmt.Println(words, len(*ps.Load()))

	// Pointer under concurrent swaps: every value is seen exactly once.
	var cur atomic.Pointer[int]
	var wg sync.WaitGroup
	sum := 0
	var mu sync.Mutex
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := i
			if o := cur.Swap(&v); o != nil {
				mu.Lock()
				sum += *o
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	fmt.Println(sum + *cur.Load())

	// Value.
	var v atomic.Value
	fmt.Println(v.Load())
	try("store nil", func() { v.Store(nil) })
	try("swap nil", func() { v.Swap(nil) })
	try("cas nil", func() { v.CompareAndSwap(nil, nil) })
	fmt.Println(v.CompareAndSwap(1, 2), v.Load())
	fmt.Println(v.CompareAndSwap(nil, 3), v.Load())
	v.Store(4)
	fmt.Println(v.Swap(5), v.Load())
	try("store string", func() { v.Store("x") })
	try("swap string", func() { v.Swap("x") })
	try("cas mixed", func() { v.CompareAndSwap(1, "x") })
	try("cas string", func() { v.CompareAndSwap(nil, "x") })
	try("cas string old", func() { v.CompareAndSwap("a", "b") })
	fmt.Println(v.CompareAndSwap(4, 6), v.CompareAndSwap(5, 6), v.Load(), v.CompareAndSwap(nil, 7), v.Load())

	var sv atomic.Value
	sv.Store(point{1, 2})
	fmt.Println(sv.CompareAndSwap(point{1, 2}, point{3, 4}), sv.Load())
	var fv atomic.Value
	fv.Store([]int{1})
	try("cas uncomparable", func() { fv.CompareAndSwap([]int{1}, []int{2}) })
	fmt.Println(fv.Load())
	var iv atomic.Value
	iv.Store(fmt.Errorf("e1"))
	try("store other error type", func() { iv.Store(fmt.Errorf("w: %w", fmt.Errorf("e"))) })
	iv.Store(fmt.Errorf("e2"))
	fmt.Println(iv.Load())
}
