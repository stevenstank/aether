package main

import (
	"fmt"
	"time"
)

func main() {
	done := make(chan struct{})
	t := time.NewTimer(50 * time.Millisecond)
	defer t.Stop()
	go func() { close(done) }()
	select {
	case <-done:
		fmt.Println("done")
	case <-t.C:
		fmt.Println("timeout")
	}
}
