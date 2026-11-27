// Cooperative preemption at loop back edges (D-059): goroutines that
// compute without ever switching, in every kind of loop, do not keep the
// others, the timers, or the collector from running, and the values they
// hold in registers when preempted are intact afterwards. Nothing printed
// depends on the schedule.
package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
)

var sink atomic.Int64

// spin never returns, and its loop calls nothing.
func spin() {
	for {
	}
}

// spinGoto loops with a backward goto until stop is set.
func spinGoto(stop *atomic.Bool, done chan<- string) {
	n := 0
again:
	n++
	if !stop.Load() {
		goto again
	}
	done <- "goto"
}

// nested runs nested loops with many values live across the inner loop's
// back edge, integer and floating-point, and returns their mix.
func nested(seed, n int) (int, float64) {
	a, b, c, d, e := seed, seed*3, seed*5+1, seed^7, seed+11
	f, g := float64(seed), 0.5
	for i := 0; i < n; i++ {
		for j := 0; j < 1000; j++ {
			a += j ^ b
			b = b*31 + c
			c ^= d >> 3
			d += e + i
			e = e*7 + a
			f = f*0.999 + float64(j&7)
			g += f * 1e-6
		}
	}
	return a ^ b ^ c ^ d ^ e, f + g
}

type node struct {
	v    [4]int
	next *node
}

// hold keeps its only pointer to a list in registers while it loops, and
// sums the list again and again; the collector runs meanwhile.
func hold(n int) int {
	var p *node
	for i := 0; i < 8; i++ {
		p = &node{v: [4]int{i, i * 2, i * 3, i * 4}, next: p}
	}
	s := 0
	for k := 0; k < n; k++ {
		for q := p; q != nil; q = q.next {
			s += q.v[0] + q.v[1] + q.v[2] + q.v[3]
		}
	}
	return s
}

// churn allocates and collects until stop is set, counting the
// collections in gcs.
func churn(stop *atomic.Bool, gcs *atomic.Int64) {
	n := 0
	for !stop.Load() {
		x := make([]*node, 64)
		for i := range x {
			x[i] = &node{v: [4]int{-1, -1, -1, -1}}
		}
		sink.Add(int64(len(x)))
		n++
		if n%50 == 0 {
			runtime.GC()
			gcs.Add(1)
		}
	}
}

func main() {
	go spin()
	go spin()

	// A timer fires while two goroutines spin.
	t0 := time.Now()
	time.Sleep(20 * time.Millisecond)
	fmt.Println("slept", time.Since(t0) >= 20*time.Millisecond)

	// A goroutine waits in a loop on a flag only main sets.
	var stop atomic.Bool
	done := make(chan string)
	go spinGoto(&stop, done)
	go func() {
		n := 0
		for !stop.Load() {
			n++
		}
		done <- "for"
	}()
	go func() {
		m := map[int]int{1: 1, 2: 2, 3: 3}
		for !stop.Load() {
			for k, v := range m {
				m[k] = v + 1
			}
		}
		done <- "range map"
	}()
	go func() {
		c := make(chan int, 4)
		for !stop.Load() {
			c <- 1
			c <- 2
			for range 2 {
				<-c
			}
		}
		done <- "range int"
	}()
	time.Sleep(5 * time.Millisecond)
	stop.Store(true)
	got := map[string]bool{}
	for range 4 {
		got[<-done] = true
	}
	fmt.Println("stopped", len(got), got["goto"], got["for"], got["range map"], got["range int"])

	// Computations preempted by each other, and by the spinners, keep
	// their registers.
	type mix struct {
		i int
		f float64
	}
	res := make(chan mix)
	for s := 1; s <= 3; s++ {
		go func() {
			i, f := nested(s, 4000)
			res <- mix{i, f}
		}()
	}
	sum, fsum := 0, 0.0
	for range 3 {
		r := <-res
		sum ^= r.i
		fsum += r.f
	}
	want, wantF := 0, 0.0
	for s := 1; s <= 3; s++ {
		i, f := nested(s, 4000)
		want ^= i
		wantF += f
	}
	fmt.Println("nested", sum == want, fsum == wantF)

	// The collector runs while a goroutine holds its list in registers.
	// Each list sums to 280.
	var cstop atomic.Bool
	var gcs atomic.Int64
	churned := make(chan bool)
	go func() { churn(&cstop, &gcs); churned <- true }()
	s, rounds := 0, 0
	for gcs.Load() < 3 {
		s += hold(100000)
		rounds++
	}
	cstop.Store(true)
	<-churned
	fmt.Println("hold", s == rounds*100000*280)

	// main returns while the spinners still spin.
	fmt.Println("done")
}
