package main

import "sync"

func main() {
	var mu sync.Mutex
	mu.Lock()
	mu.Unlock()
	defer println("not printed: a fatal error runs no deferred call")
	defer func() { recover() }()
	mu.Unlock()
}
