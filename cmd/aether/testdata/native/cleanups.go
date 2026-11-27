// cleanups stresses weak pointers and runtime.AddCleanup over Aether's
// collector (D-057, D-058) against gc, over many rounds of objects made
// and dropped: weak pointers of reachable objects, interior ones
// included, are never cleared, and their cleanups never run; each other
// cleanup runs at most once, never after Stop, and most of them run,
// some adding cleanups and weak pointers of their own; memory stays
// bounded. main never blocks: runtime.GC and Gosched are its only
// switches, and must start the cleanups, as gc's run on their own, and
// run a due timer. Objects are made in frames that have returned, whose
// stack is then overwritten, so that the conservative collector (D-046)
// finds no stale pointer to them.
package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"weak"
)

type T struct {
	id   int
	pad  [6]int
	next *T
}

// gc runs cleanups concurrently: what they record is locked.
var (
	mu           sync.Mutex
	ranFor       []uint8 // runs of each object's cleanup
	stopped      []bool
	ranKept      int
	ranAfterStop int

	kept     []*T
	keptWeak []weak.Pointer[T]
	keptPad  []weak.Pointer[int] // of an interior word
)

//go:noinline
func round(r, n int) {
	var chain *T
	for i := range n {
		id := r*n + i
		t := &T{id: id}
		w := weak.Make(t)
		if weak.Make(t) != w || w.Value() != t || weak.Make(&t.pad[2]) == weak.Make(&t.pad[3]) {
			panic("handles")
		}
		if i%97 == 0 {
			kept = append(kept, t)
			keptWeak = append(keptWeak, w)
			keptPad = append(keptPad, weak.Make(&t.pad[5]))
			runtime.AddCleanup(t, func(int) {
				mu.Lock()
				ranKept++
				mu.Unlock()
			}, id)
			continue
		}
		if i%5 == 0 {
			t.next = chain // reachable only from other dropped objects
			chain = t
		}
		c := runtime.AddCleanup(t, func(id int) {
			mu.Lock()
			ranFor[id]++
			if stopped[id] {
				ranAfterStop++
			}
			mu.Unlock()
		}, id)
		if i%7 == 0 {
			c.Stop()
			mu.Lock()
			stopped[id] = true
			mu.Unlock()
		}
		if i%11 == 0 {
			runtime.AddCleanup(t, func(id int) {
				u := &T{id: -id}
				runtime.AddCleanup(u, func(int) {}, 0)
				_ = weak.Make(u)
			}, id)
		}
	}
}

//go:noinline
func clobber(n int) int {
	var buf [256]int
	for i := range buf {
		buf[i] = n
	}
	if n > 0 {
		return clobber(n-1) + buf[n%256]
	}
	return buf[0]
}

// rss is the resident set's bytes.
func rss() int {
	b, _ := os.ReadFile("/proc/self/statm")
	var size, res int
	fmt.Sscan(string(b), &size, &res)
	return res * 4096
}

func main() {
	// A due timer fires while main yields with nothing else to run.
	var fired atomic.Bool
	time.AfterFunc(5*time.Millisecond, func() { fired.Store(true) })
	for start := time.Now(); !fired.Load() && time.Since(start) < 5*time.Second; {
		runtime.Gosched()
	}
	fmt.Println("timer fired while yielding:", fired.Load())

	const rounds, n = 100, 500
	ranFor, stopped = make([]uint8, rounds*n), make([]bool, rounds*n)
	var mem0 int
	for r := range rounds {
		round(r, n)
		clobber(40)
		runtime.GC()
		for range 20 {
			runtime.Gosched()
		}
		if r == 4 {
			mem0 = rss()
		}
	}
	runtime.GC()
	for range 20 {
		runtime.Gosched()
	}
	mem := rss()
	mu.Lock()
	defer mu.Unlock()
	intact := true
	for i, w := range keptWeak {
		if w.Value() != kept[i] || keptPad[i].Value() != &kept[i].pad[5] {
			intact = false
		}
	}
	once, ran := true, 0
	for _, c := range ranFor {
		if c > 1 {
			once = false
		}
		if c > 0 {
			ran++
		}
	}
	dropped := rounds * (n - (n+96)/97)
	fmt.Println("kept intact:", intact, "kept cleanups run:", ranKept)
	fmt.Println("each cleanup at most once:", once, "after Stop:", ranAfterStop)
	fmt.Println("most dropped cleaned up:", ran*2 > dropped)
	fmt.Println("memory bounded:", mem < 2*mem0+8<<20)
}
