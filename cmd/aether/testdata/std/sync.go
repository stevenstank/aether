package main

import (
	"runtime"
	"sync"
)

type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) add(k int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.n
	runtime.Gosched() // others try to lock meanwhile
	c.n = v + k
}

func recovered(f func()) {
	defer func() {
		println("recovered:", recover().(string))
	}()
	f()
}

func main() {
	// A Mutex and a WaitGroup.
	var c counter
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.add(i)
		}()
	}
	wg.Wait()
	println(c.n)

	var mu sync.Mutex
	println(mu.TryLock(), mu.TryLock())
	mu.Unlock()
	println(mu.TryLock())
	mu.Unlock()

	// An RWMutex: readers share it, a writer excludes them.
	var rw sync.RWMutex
	shared := 0
	readers := 0
	most := 0
	var wg2 sync.WaitGroup
	for i := range 10 {
		wg2.Add(1)
		if i%3 == 0 {
			go func() {
				defer wg2.Done()
				rw.Lock()
				if readers != 0 {
					println("writer with readers")
				}
				v := shared
				runtime.Gosched()
				shared = v + 1
				rw.Unlock()
			}()
			continue
		}
		go func() {
			defer wg2.Done()
			rw.RLock()
			readers++
			most = max(most, readers)
			runtime.Gosched()
			readers--
			rw.RUnlock()
		}()
	}
	wg2.Wait()
	println(shared, readers, most >= 1)
	println(rw.TryRLock(), rw.TryRLock(), rw.TryLock())
	rw.RUnlock()
	rw.RUnlock()
	println(rw.TryLock(), rw.TryRLock())
	rw.Unlock()
	var l sync.Locker = rw.RLocker()
	l.Lock()
	println(rw.TryLock())
	l.Unlock()
	println(rw.TryLock())
	rw.Unlock()
	l = &mu
	l.Lock()
	println(mu.TryLock())
	l.Unlock()

	// A WaitGroup waited on by several goroutines.
	var start, finish sync.WaitGroup
	start.Add(1)
	results := make(chan int, 3)
	for i := range 3 {
		finish.Add(1)
		go func() {
			defer finish.Done()
			start.Wait()
			results <- i * i
		}()
	}
	start.Done()
	finish.Wait()
	println(<-results + <-results + <-results)
	start.Wait() // a zero counter returns at once

	// Once, also when f panics.
	var once sync.Once
	calls := 0
	for range 3 {
		once.Do(func() { calls++ })
	}
	println(calls)
	var once2 sync.Once
	recovered(func() { once2.Do(func() { panic("in Do") }) })
	once2.Do(func() { println("not called") })
	println("done")

	var bad sync.WaitGroup
	recovered(func() { bad.Done() })
}
