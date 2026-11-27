// fairness checks the scheduler's fairness (D-058) against gc: goroutines
// that keep running, and switch only through Gosched, delay neither
// connections ready to read, on several connections at once, nor a
// goroutine waiting in the run queue while two others pass a value back
// and forth. Aether has no preemption (D-049): a ready descriptor waits
// until a switch, at most about the time the busy goroutines run between
// switches (1 ms here), not a fixed number of switches.
package main

import (
	"fmt"
	"net"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// spin computes for d without switching.
func spin(d time.Duration) int {
	x := 0
	for end := time.Now().Add(d); time.Now().Before(end); {
		for i := range 1000 {
			x += i
		}
	}
	return x
}

func echo(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			b := make([]byte, 16)
			for {
				n, err := c.Read(b)
				if err != nil {
					return
				}
				c.Write(b[:n])
			}
		}()
	}
}

func main() {
	// A goroutine in the run queue runs while two others pass a value
	// back and forth, each making the other runnext.
	a, b := make(chan int), make(chan int)
	go func() {
		for v := range a {
			b <- v
		}
	}()
	a <- 0
	<-b
	runtime.Gosched() // the partner now waits to receive from a
	var ran atomic.Bool
	go func() { ran.Store(true) }()
	start := time.Now()
	n := 0
	for !ran.Load() && time.Since(start) < 5*time.Second {
		a <- n
		<-b
		n++
	}
	close(a)
	fmt.Println("queued goroutine ran:", ran.Load(), "within 2s:", time.Since(start) < 2*time.Second)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	go echo(ln)
	var stop atomic.Bool
	var busy sync.WaitGroup
	for range 3 {
		busy.Add(1)
		go func() {
			defer busy.Done()
			for !stop.Load() {
				spin(time.Millisecond)
				runtime.Gosched()
			}
		}()
	}
	const clients, trips = 6, 15
	var mu sync.Mutex
	var lat []time.Duration
	var wg sync.WaitGroup
	for i := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				panic(err)
			}
			defer c.Close()
			b := []byte{byte('a' + i)}
			for range trips {
				t := time.Now()
				if _, err := c.Write(b); err != nil {
					panic(err)
				}
				if _, err := c.Read(b); err != nil || b[0] != byte('a'+i) {
					panic(fmt.Sprint("echo: ", err, b))
				}
				d := time.Since(t)
				mu.Lock()
				lat = append(lat, d)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	stop.Store(true)
	busy.Wait()
	slices.Sort(lat)
	// Before D-058 a poll came every 61 switches: about 120 ms a trip.
	fmt.Println("round trips:", len(lat), "median under 25ms:", lat[len(lat)/2] < 25*time.Millisecond)
	ln.Close()
}
