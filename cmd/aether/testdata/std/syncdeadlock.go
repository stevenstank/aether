package main

import "sync"

func main() {
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mu.Lock()
	}()
	mu.Lock()
	wg.Wait()
	println("both goroutines ran")
	mu.Lock()
}
