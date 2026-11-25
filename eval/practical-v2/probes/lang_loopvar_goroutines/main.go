package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	out := make([]int, 4)
	for c := 0; c < 4; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[c] = c * c
		}()
	}
	wg.Wait()
	fmt.Println(out)
}
