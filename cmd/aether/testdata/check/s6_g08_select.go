// aether:expect run

package main

// select statements whose output does not depend on the schedule
// (D-048, increment 10.3): default, nil channels, a fan-in that drops
// closed channels, send cases, break and continue, comma-ok, assignment
// to variables and map elements, operands evaluated once in source order,
// a send on a closed channel recovered, and select {} in a goroutine.

type P struct {
	a int8
	b int64
}

func ch(n int, c chan int) chan int { print("c", n, " "); return c }
func val(n int) int                { print("v", n, " "); return n }

func main() {
	e := make(chan int)
	select {
	case v := <-e:
		println("got", v)
	default:
		println("default")
	}
	var nilc chan int
	b := make(chan int, 1)
	b <- 7
	select {
	case <-nilc:
		println("nil")
	case nilc <- 1:
		println("nil send")
	case v, ok := <-b:
		println("b", v, ok)
	}
	select {
	case <-nilc:
	default:
		println("all nil")
	}

	a1, a2 := make(chan int), make(chan int)
	go func() {
		for i := 1; i <= 100; i++ {
			a1 <- i
		}
		close(a1)
	}()
	go func() {
		for i := 1; i <= 50; i++ {
			a2 <- i * 1000
		}
		close(a2)
	}()
	sum, n := 0, 0
	for a1 != nil || a2 != nil {
		select {
		case v, ok := <-a1:
			if !ok {
				a1 = nil
				break
			}
			sum += v
			n++
		case v, ok := <-a2:
			if !ok {
				a2 = nil
				continue
			}
			sum += v
			n++
		}
	}
	println(sum, n)

	out := make(chan P)
	quit := make(chan bool)
	go func() {
		s := 0
		for {
			select {
			case p := <-out:
				s += int(p.a) + int(p.b)
			case <-quit:
				println("worker", s)
				quit <- true
				return
			}
		}
	}()
	for i := 0; i < 10; i++ {
		select {
		case out <- P{int8(i), int64(i * 10)}:
		}
	}
	quit <- true
	<-quit

	var x int
	var ok bool
	m := map[string]int{}
	c := make(chan int, 2)
	c <- 5
	close(c)
	select {
	case x, ok = <-c:
	}
	select {
	case m["k"] = <-c:
	}
	println(x, ok, m["k"], len(m))

	d := make(chan int, 1)
	select {
	case ch(1, nil) <- val(2):
	case <-ch(3, nil):
	case ch(4, d) <- val(5):
	}
	println(<-d)

	func() {
		defer func() { println(recover().(error).Error()) }()
		cl := make(chan int)
		close(cl)
		select {
		case cl <- 1:
		case <-nilc:
		}
	}()
	go func() { select {} }()
	println("end")
}
