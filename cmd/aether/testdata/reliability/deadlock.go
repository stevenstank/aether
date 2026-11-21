// Workers finish, then main waits on a channel nobody will send to while
// every other goroutine is blocked: gc's deadlock fatal error.
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	results := make(chan int, 10)
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- i * i
		}()
	}
	wg.Wait()
	close(results)
	s := 0
	for v := range results {
		s += v
	}
	fmt.Println("sum", s)
	var mu sync.Mutex
	mu.Lock()
	block := make(chan int)
	go func() {
		mu.Lock()
		block <- 1
	}()
	<-block
}
