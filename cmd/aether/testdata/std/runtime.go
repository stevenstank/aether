package main

import "runtime"

type node struct {
	next *node
	val  int
}

func build(n int) *node {
	var l *node
	for i := range n {
		l = &node{l, i}
	}
	return l
}

func sum(l *node) int {
	s := 0
	for ; l != nil; l = l.next {
		s += l.val
	}
	return s
}

func main() {
	println(runtime.GOOS, runtime.GOARCH, runtime.NumGoroutine())

	// Goroutines exist from their go statements until they return.
	block, done := make(chan int), make(chan int)
	for i := range 5 {
		go func() {
			<-block
			done <- i
		}()
	}
	println(runtime.NumGoroutine())
	close(block)
	total := 0
	for range 5 {
		total += <-done
	}
	for runtime.NumGoroutine() > 1 {
		runtime.Gosched()
	}
	println(total, runtime.NumGoroutine())

	// Gosched with no other goroutine returns.
	runtime.Gosched()

	// Gosched lets the other goroutines run: the worker finishes its
	// steps while main yields, whatever their schedule.
	steps := make(chan int, 100)
	go func() {
		for i := range 10 {
			steps <- i
			runtime.Gosched()
		}
		close(steps)
	}()
	n := 0
	for s := range steps {
		n += s
		runtime.Gosched()
	}
	println(n)

	// Collections keep what is reachable.
	l := build(1000)
	for range 3 {
		build(10000)
		runtime.GC()
	}
	println(sum(l))

	// GC lets another goroutine run while it collects.
	stop := make(chan bool)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				runtime.GC()
			}
		}
	}()
	k := 0
	for i := range 50 {
		runtime.Gosched()
		k += i
	}
	stop <- true
	println(k)
}
