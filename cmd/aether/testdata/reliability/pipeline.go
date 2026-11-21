// A concurrent pipeline: generator, fan-out to squaring workers, fan-in
// with select over several channels, a quit channel, and a semaphore made
// of a buffered channel. Results are sorted before printing.
package main

import (
	"fmt"
	"slices"
	"sync"
)

func gen(n int, quit <-chan struct{}) <-chan int {
	out := make(chan int)
	go func() {
		defer func() { close(out) }()
		for i := 1; i <= n; i++ {
			select {
			case out <- i:
			case <-quit:
				return
			}
		}
	}()
	return out
}

func square(id int, in <-chan int) <-chan [2]int {
	out := make(chan [2]int, 4)
	go func() {
		defer func() { close(out) }()
		for v := range in {
			out <- [2]int{id, v * v}
		}
	}()
	return out
}

func merge(cs ...<-chan [2]int) <-chan [2]int {
	out := make(chan [2]int)
	var wg sync.WaitGroup
	wg.Add(len(cs))
	for _, c := range cs {
		go func(c <-chan [2]int) {
			defer wg.Done()
			for v := range c {
				out <- v
			}
		}(c)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

type stage func(int) int

func chain(in <-chan int, stages ...stage) <-chan int {
	for _, s := range stages {
		c := make(chan int)
		go func(in <-chan int, s stage) {
			defer func() { close(c) }()
			for v := range in {
				c <- s(v)
			}
		}(in, s)
		in = c
	}
	return in
}

func main() {
	quit := make(chan struct{})
	src := gen(500, quit)
	workers := make([]<-chan [2]int, 5)
	for i := range workers {
		workers[i] = square(i, src)
	}
	var vals []int
	sum := 0
	for p := range merge(workers...) {
		vals = append(vals, p[1])
		sum += p[1]
	}
	slices.Sort(vals)
	fmt.Println("squares:", len(vals), "sum:", sum, "first:", vals[:5], "last:", vals[len(vals)-2:])

	// Early quit: the consumer stops after 10 values.
	quit2 := make(chan struct{})
	g := gen(1_000_000, quit2)
	got := 0
	for v := range g {
		got += v
		if v == 10 {
			close(quit2)
			break
		}
	}
	for range g {
	}
	fmt.Println("early quit sum:", got)

	out := chain(gen(100, quit), func(x int) int { return x + 1 }, func(x int) int { return x * 3 }, func(x int) int { return x % 17 })
	hist := make([]int, 17)
	for v := range out {
		hist[v]++
	}
	fmt.Println("chain hist:", hist)

	// A semaphore bounds concurrency; the maximum observed is checked.
	sem := make(chan struct{}, 3)
	var mu sync.Mutex
	active, peak := 0, 0
	var wg sync.WaitGroup
	results := make([]int, 40)
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			mu.Lock()
			active++
			peak = max(peak, active)
			mu.Unlock()
			s := 0
			for j := 0; j <= i*100; j++ {
				s += j % 7
			}
			results[i] = s
			mu.Lock()
			active--
			mu.Unlock()
			<-sem
		}()
	}
	wg.Wait()
	fmt.Println("peak <= 3:", peak <= 3, "peak >= 1:", peak >= 1, "results:", results[0], results[39])

	// select with default and multiple ready channels.
	a, b := make(chan int, 10), make(chan int, 10)
	for i := range 10 {
		a <- i
		b <- 100 + i
	}
	fromA, fromB, idle := 0, 0, 0
	for fromA+fromB < 20 {
		select {
		case v := <-a:
			fromA += 1 + v - v
		case _, ok := <-b:
			if ok {
				fromB++
			}
		default:
			idle++
		}
	}
	close(a)
	v, ok := <-a
	fmt.Println("select:", fromA, fromB, idle, v, ok)

	// sync.Once and a nil channel case that never fires.
	var once sync.Once
	var nilc chan int
	calls := 0
	for range 5 {
		once.Do(func() { calls++ })
		select {
		case <-nilc:
			calls += 100
		default:
		}
	}
	fmt.Println("once:", calls)
}
