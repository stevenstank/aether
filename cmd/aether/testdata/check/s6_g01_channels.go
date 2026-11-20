// aether:expect run

package main

// Goroutines and channels whose output does not depend on the schedule
// (D-048): unbuffered and buffered channels of every kind of element,
// close, comma-ok, len, cap, nil and comparison, directional channels,
// and range.

type P struct {
	a int8
	b int64
	s string
}

type Shape interface{ Area() int }

type Sq struct{ n int }

func (s Sq) Area() int { return s.n * s.n }

func worker(id int, in <-chan int, out chan<- int) {
	for v := range in {
		out <- v * id
	}
	close(out)
}

func main() {
	c := make(chan int)
	go func() { c <- 42 }()
	println(<-c)

	b := make(chan string, 2)
	b <- "x"
	b <- "y"
	println(len(b), cap(b), <-b, <-b, len(b))

	in, out := make(chan int), make(chan int, 3)
	go worker(3, in, out)
	for i := 1; i <= 3; i++ {
		in <- i
	}
	close(in)
	s := 0
	for v := range out {
		s += v
	}
	println(s)
	v, ok := <-out
	println(v, ok)

	ps := make(chan P, 1)
	go func() { ps <- P{1, 2, "three"} }()
	p := <-ps
	println(p.a, p.b, p.s)
	go func(q P) { ps <- q }(P{-4, 5, "six"})
	println((<-ps).s)

	shapes := make(chan Shape)
	go func() {
		shapes <- Sq{3}
		shapes <- nil
		close(shapes)
	}()
	for sh := range shapes {
		if sh == nil {
			println("nil shape")
			continue
		}
		println(sh.Area())
	}

	arrs := make(chan [3]int, 2)
	arrs <- [3]int{1, 2, 3}
	sl := make(chan []int, 1)
	sl <- []int{7, 8}
	a := <-arrs
	println(a[0]+a[1]+a[2], len(<-sl))

	done := make(chan struct{})
	go func() { println("in goroutine"); done <- struct{}{} }()
	<-done
	go func() { close(done) }()
	<-done
	println("closed")

	var nilc chan int
	println(len(nilc), cap(nilc), nilc == nil, c != nil, c == c)
	cc := make(chan chan int, 1)
	cc <- c
	go func() { c <- 9 }()
	println(<-<-cc)
	m := map[chan int]int{c: 1, nilc: 2}
	println(m[c], m[nilc], len(m))

	o2 := make(chan int, 2)
	o2 <- 5
	o2 <- 6
	close(o2)
	var x int
	for x = range o2 {
	}
	println(x)
	count := 0
	n := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		n <- i%2 == 0
	}
	close(n)
	for range n {
		count++
	}
	println(count)
}
