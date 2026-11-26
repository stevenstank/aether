// weak checks weak pointers and runtime.AddCleanup over Aether's collector
// (D-057): handles equal for equal pointers, cleared once their object is
// collected, kept while it is reachable; cleanups run once, on a goroutine
// of their own, unless stopped, and never for a reachable object. Objects
// are made and dropped in frames that have returned, whose stack is then
// overwritten, so that the conservative collector (D-046) finds no stale
// pointer to them.
package main

import (
	"fmt"
	"runtime"
	"sort"
	"time"
	"weak"
)

type T struct {
	name string
	pad  [40]byte
}

var (
	kept    []*T
	ran     = make(chan string, 100)
	dropped []weak.Pointer[T]
	held    []weak.Pointer[T]
)

//go:noinline
func setup() {
	for i := range 200 {
		t := &T{name: fmt.Sprint("t", i)}
		w := weak.Make(t)
		if w != weak.Make(t) || w.Value() != t {
			panic("handle")
		}
		if i%50 == 0 {
			kept = append(kept, t)
			held = append(held, w)
			runtime.AddCleanup(t, func(s string) { ran <- "kept " + s }, t.name)
			continue
		}
		dropped = append(dropped, w)
		c := runtime.AddCleanup(t, func(s string) { ran <- s }, t.name)
		if i%10 == 1 {
			c.Stop()
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

func main() {
	fmt.Println(weak.Make[T](nil).Value() == nil)
	setup()
	clobber(50)
	runtime.GC()
	runtime.GC()
	var names []string
	timeout := time.After(10 * time.Second)
	for len(names) < 176 {
		select {
		case s := <-ran:
			names = append(names, s)
		case <-timeout:
			fmt.Println("timeout after", len(names))
			return
		}
	}
	time.Sleep(20 * time.Millisecond)
	cleared := 0
	for _, w := range dropped {
		if w.Value() == nil {
			cleared++
		}
	}
	live := 0
	for i, w := range held {
		if w.Value() == kept[i] {
			live++
		}
	}
	sort.Strings(names)
	fmt.Println(len(names), len(ran), names[0], names[len(names)-1], cleared, len(dropped), live)
}
