// A preemption at a function's entry (D-060) is invisible to recover: a
// deferred call preempted as it enters still recovers its panic, though
// the scheduler's hooks, with no other goroutine runnable, run functions
// meanwhile. Before the scheduler restored .rframe, the recover returned
// nil.
package main

import (
	"fmt"
	"time"
)

// try returns i through a panic its deferred call recovers.
func try(i int) (r int) {
	defer func() { r = recover().(int) }()
	panic(i)
}

func main() {
	// A goroutine that has ended and a timer: a scheduler with hooks.
	done := make(chan bool)
	go func() { done <- true }()
	<-done
	time.Sleep(time.Millisecond)
	s := 0
	for i := 0; i < 1000000; i++ {
		s += try(i)
	}
	fmt.Println(s)
}
