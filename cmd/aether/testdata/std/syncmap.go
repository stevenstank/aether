package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%s: panic: %v\n", name, r)
		}
	}()
	f()
}

func dump(m *sync.Map) string {
	var parts []string
	m.Range(func(k, v any) bool {
		parts = append(parts, fmt.Sprintf("%v=%v", k, v))
		return true
	})
	slices.Sort(parts)
	return "{" + strings.Join(parts, " ") + "}"
}

type key struct {
	a string
	b int
}

func main() {
	var m sync.Map
	fmt.Println(m.Load("missing"))
	fmt.Println(dump(&m))
	m.Store("a", 1)
	m.Store(key{"k", 1}, "struct")
	m.Store(2, []int{1, 2})
	m.Store(nil, "nil key")
	fmt.Println(m.Load("a"))
	fmt.Println(m.Load(key{"k", 1}))
	fmt.Println(m.Load(nil))
	fmt.Println(m.LoadOrStore("a", 9))
	fmt.Println(m.LoadOrStore("b", 9))
	fmt.Println(m.Swap("a", 10))
	fmt.Println(m.Swap("c", 11))
	fmt.Println(m.LoadAndDelete("c"))
	fmt.Println(m.LoadAndDelete("c"))
	m.Delete("nothing")
	fmt.Println(m.CompareAndSwap("a", 1, 2), m.CompareAndSwap("a", 10, 12), m.CompareAndSwap("zz", nil, 1))
	fmt.Println(m.CompareAndSwap("a", "12", 1), m.CompareAndDelete("b", 8), m.CompareAndDelete("b", 9))
	fmt.Println(dump(&m))
	try("uncomparable old", func() { m.CompareAndSwap(2, []int{1, 2}, 0) })
	try("unhashable key", func() { m.Store([]int{1}, 1) })
	try("unhashable load", func() { m.Load(map[int]int{}) })
	fmt.Println(m.CompareAndSwap(2, "other type", 0))

	// Range stops early, and tolerates deletion.
	n := 0
	m.Range(func(k, v any) bool {
		n++
		return n < 2
	})
	fmt.Println("visited", n)
	m.Range(func(k, v any) bool {
		m.Delete(k)
		return true
	})
	fmt.Println(dump(&m))
	m.Store("x", 1)
	m.Clear()
	fmt.Println(dump(&m))

	// Concurrent counters with LoadOrStore and CompareAndSwap.
	var counts sync.Map
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprint("k", i%4)
			for {
				v, _ := counts.LoadOrStore(k, 0)
				if counts.CompareAndSwap(k, v, v.(int)+1) {
					return
				}
			}
		}()
	}
	wg.Wait()
	fmt.Println(dump(&counts))

	// OnceFunc, OnceValue, and OnceValues.
	calls := 0
	f := sync.OnceFunc(func() { calls++ })
	f()
	f()
	fmt.Println("calls", calls)
	g := sync.OnceValue(func() int { calls++; return calls * 10 })
	fmt.Println(g(), g(), calls)
	h := sync.OnceValues(func() (string, error) { calls++; return "v", errors.New("e") })
	s, err := h()
	s2, err2 := h()
	fmt.Println(s, err, s2, err2 == err, calls)
	bad := sync.OnceFunc(func() { calls++; panic("boom") })
	try("first", bad)
	try("second", bad)
	badv := sync.OnceValue(func() int { panic(fmt.Errorf("err %d", 7)) })
	try("value first", func() { badv() })
	try("value second", func() { badv() })
	badvs := sync.OnceValues(func() (int, int) { panic(42) })
	try("values", func() { badvs() })
	fmt.Println("calls", calls)
}
