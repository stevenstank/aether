// timers checks Sleep, After, NewTimer, AfterFunc, and Ticker in the
// scheduler (D-054): gc's results for Stop and Reset of synchronous timer
// channels, len and cap 0, a select polling a deadline, and only elapsed
// times' bounds.
package main

import (
	"fmt"
	"time"
)

func main() {
	start := time.Now()
	done := make(chan string)
	go func() {
		time.Sleep(30 * time.Millisecond)
		done <- "slept"
	}()
	select {
	case s := <-done:
		fmt.Println(s)
	case <-time.After(2 * time.Second):
		fmt.Println("timeout")
	}
	select {
	case <-make(chan int):
	case <-time.After(20 * time.Millisecond):
		fmt.Println("after")
	}
	t := time.NewTimer(time.Hour)
	fmt.Println(len(t.C), cap(t.C), t.Stop(), t.Stop())
	fmt.Println(t.Reset(10*time.Millisecond), t.Reset(10*time.Millisecond))
	<-t.C
	fmt.Println("fired", t.Stop(), t.Reset(time.Millisecond))
	time.Sleep(5 * time.Millisecond)
	fmt.Println("expired unreceived stop:", t.Stop())
	got := make(chan int, 3)
	time.AfterFunc(5*time.Millisecond, func() { got <- 1 })
	fmt.Println(<-got)
	tk := time.NewTicker(3 * time.Millisecond)
	n := 0
	for range tk.C {
		n++
		if n == 4 {
			tk.Stop()
			break
		}
	}
	fmt.Println("ticks", n)
	deadline := time.After(15 * time.Millisecond)
	spins := 0
poll:
	for {
		select {
		case <-deadline:
			break poll
		default:
			spins++
		}
	}
	fmt.Println("polled", spins > 0)
	el := time.Since(start)
	fmt.Println(el > 60*time.Millisecond, el < 2*time.Second)
	var zero time.Timer
	defer func() { fmt.Println("recovered:", recover()) }()
	zero.Stop()
}
