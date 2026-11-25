package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.RWMutex
	var once sync.Once
	var wg sync.WaitGroup
	n := 0
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			once.Do(func() { mu.Lock(); n++; mu.Unlock() })
		}()
	}
	wg.Wait()
	mu.RLock()
	fmt.Println(n)
	mu.RUnlock()
}
