// aether:expect run

package main

// The channel panics, recovered as gc's plain errors, and each
// goroutine's own panic state: main communicates with a goroutine that
// panics and recovers while main's own panic is in progress (D-048).

func try(name string, f func()) {
	defer func() {
		r := recover()
		rt, isRT := r.(interface{ RuntimeError() })
		println(name, r.(error).Error(), isRT, rt != nil)
	}()
	f()
}

func main() {
	try("send", func() {
		c := make(chan int, 1)
		close(c)
		c <- 1
	})
	try("close nil", func() {
		var c chan string
		close(c)
	})
	try("close closed", func() {
		c := make(chan bool)
		close(c)
		close(c)
	})
	n := -1
	try("make", func() { println(len(make(chan int, n))) })

	// A sender, blocked or not yet sending, and a receiver, blocked or
	// not, when the channel is closed: the same output either way.
	c, r := make(chan int), make(chan int)
	sent, received := make(chan string), make(chan string)
	go func() {
		defer func() { sent <- recover().(error).Error() }()
		c <- 1
	}()
	rv, rok := 7, true
	go func() {
		rv, rok = <-r
		received <- "received"
	}()
	close(c)
	close(r)
	println(<-sent)
	println(<-received, rv, rok)

	ch := make(chan string)
	go func() {
		s := <-ch
		defer func() {
			println("goroutine recovers", recover().(string))
			ch <- s + "!"
		}()
		panic("goroutine's own")
	}()
	func() {
		defer func() {
			ch <- "hello"
			println("from goroutine", <-ch)
			println("main recovers", recover().(string))
		}()
		panic("main's own")
	}()
	println("end")
}
