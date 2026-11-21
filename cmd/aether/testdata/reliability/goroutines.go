// Concurrency stress: many goroutines, unbuffered ping-pong, a token ring,
// a concurrent prime sieve, readers and writers under an RWMutex, atomic
// compare-and-swap loops, select over many channels, and goroutines that
// allocate while others hold pointers, so the collector scans suspended
// stacks.
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

func fanIn(n int) int {
	c := make(chan int)
	for i := range n {
		go func() { c <- i }()
	}
	s := 0
	for range n {
		s += <-c
	}
	return s
}

func pingPong(rounds int) int {
	ping, pong := make(chan int), make(chan int)
	go func() {
		for v := range ping {
			pong <- v + 1
		}
		close(pong)
	}()
	v := 0
	for range rounds {
		ping <- v
		v = <-pong
	}
	close(ping)
	_, ok := <-pong
	if ok {
		return -1
	}
	return v
}

func ring(n, laps int) int {
	first := make(chan int)
	in := first
	for range n - 1 {
		out := make(chan int)
		go func(in, out chan int) {
			for v := range in {
				out <- v + 1
			}
			close(out)
		}(in, out)
		in = out
	}
	last := in
	hops := 0
	for range laps {
		first <- hops
		hops = <-last
	}
	close(first)
	for range last {
	}
	return hops
}

func sieve(n int) []int {
	gen := func() chan int {
		c := make(chan int)
		go func() {
			for i := 2; ; i++ {
				c <- i
			}
		}()
		return c
	}
	filter := func(in chan int, p int) chan int {
		out := make(chan int)
		go func() {
			for {
				if v := <-in; v%p != 0 {
					out <- v
				}
			}
		}()
		return out
	}
	var primes []int
	c := gen()
	for range n {
		p := <-c
		primes = append(primes, p)
		c = filter(c, p)
	}
	return primes
}

type table struct {
	mu   sync.RWMutex
	data map[int][]int
	sum  int
}

func rw(writers, readers, ops int) (int, int, bool) {
	t := &table{data: map[int][]int{}}
	var wg sync.WaitGroup
	var bad atomic.Bool
	var reads atomic.Int64
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ops {
				t.mu.Lock()
				k := (w*ops + i) % 97
				t.data[k] = append(t.data[k], i)
				t.sum += i
				t.mu.Unlock()
				if i%16 == 0 {
					runtime.Gosched()
				}
			}
		}()
	}
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range ops {
				t.mu.RLock()
				s := 0
				for _, v := range t.data {
					for _, x := range v {
						s += x
					}
				}
				if s != t.sum {
					bad.Store(true)
				}
				t.mu.RUnlock()
				reads.Add(1)
				runtime.Gosched()
			}
		}()
	}
	wg.Wait()
	return t.sum, int(reads.Load()), bad.Load()
}

func cas(workers, n int) int64 {
	var v atomic.Int64
	var raw int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range n {
				for {
					old := v.Load()
					if v.CompareAndSwap(old, old+1) {
						break
					}
				}
				atomic.AddInt64(&raw, 2)
			}
		}()
	}
	wg.Wait()
	return v.Load()*1000000 + atomic.LoadInt64(&raw)
}

func multiSelect(per int) [6]int {
	var cs [6]chan int
	for i := range cs {
		cs[i] = make(chan int, i)
	}
	var wg sync.WaitGroup
	for i := range cs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range per {
				cs[i] <- j
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	var got [6]int
	total := 0
	for total < 6*per {
		select {
		case <-cs[0]:
			got[0]++
		case <-cs[1]:
			got[1]++
		case <-cs[2]:
			got[2]++
		case <-cs[3]:
			got[3]++
		case <-cs[4]:
			got[4]++
		case <-cs[5]:
			got[5]++
		}
		total++
	}
	<-done
	return got
}

type node struct {
	next *node
	val  []byte
}

func allocators(workers, n int) int {
	results := make(chan int, workers)
	var mu sync.Mutex
	shared := map[int]*node{}
	for w := range workers {
		go func() {
			var head *node
			for i := range n {
				head = &node{head, make([]byte, 8+i%64)}
				head.val[0] = byte(w)
				if i%100 == 0 {
					mu.Lock()
					shared[w*n+i] = head
					mu.Unlock()
					runtime.Gosched()
				}
				if i%500 == 499 {
					head = nil
				}
			}
			s := 0
			for p := head; p != nil; p = p.next {
				s += int(p.val[0]) + len(p.val)
			}
			results <- s
		}()
	}
	total := 0
	for range workers {
		total += <-results
	}
	runtime.GC()
	mu.Lock()
	defer mu.Unlock()
	for _, p := range shared {
		for ; p != nil; p = p.next {
			total += int(p.val[0])
		}
	}
	return total
}

func main() {
	fmt.Println("fan-in:", fanIn(2000))
	fmt.Println("ping-pong:", pingPong(20000))
	fmt.Println("ring:", ring(300, 40))
	p := sieve(200)
	fmt.Println("primes:", len(p), p[:10], p[len(p)-1])
	sum, reads, bad := rw(4, 8, 300)
	fmt.Println("rwmutex:", sum, reads, bad)
	fmt.Println("cas:", cas(16, 2000))
	fmt.Println("select:", multiSelect(500))
	fmt.Println("allocators:", allocators(12, 3000))
	fmt.Println("goroutines left > 0:", runtime.NumGoroutine() > 1)
}
