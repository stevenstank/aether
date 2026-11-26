// synccond checks sync.Cond (D-057): Wait releases the lock and takes it
// again, Signal wakes the goroutine that waited longest, Broadcast all.
package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	c := sync.NewCond(&mu)
	var queue []int
	var wg sync.WaitGroup
	got := make([][]int, 3)
	for w := range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			defer mu.Unlock()
			for {
				for len(queue) == 0 {
					c.Wait()
				}
				v := queue[0]
				queue = queue[1:]
				if v < 0 {
					return
				}
				got[w] = append(got[w], v)
			}
		}()
	}
	for i := range 9 {
		mu.Lock()
		queue = append(queue, i)
		mu.Unlock()
		c.Signal()
	}
	mu.Lock()
	queue = append(queue, -1, -1, -1)
	mu.Unlock()
	c.Broadcast()
	wg.Wait()
	total := 0
	for _, g := range got {
		total += len(g)
	}
	fmt.Println(total, len(queue))

	// Broadcast wakes every waiter; Signal with none waiting does nothing.
	ready := false
	var woke sync.WaitGroup
	n := 0
	for range 4 {
		woke.Add(1)
		go func() {
			defer woke.Done()
			mu.Lock()
			for !ready {
				c.Wait()
			}
			n++
			mu.Unlock()
		}()
	}
	c.Signal()
	mu.Lock()
	ready = true
	mu.Unlock()
	c.Broadcast()
	woke.Wait()
	fmt.Println(n)
}
