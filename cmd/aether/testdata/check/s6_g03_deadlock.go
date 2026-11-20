// aether:expect run

package main

// Every goroutine blocked: gc's fatal error, after the output so far
// (D-048).

func main() {
	c, d := make(chan int), make(chan int)
	go func() { <-d }()
	go func() {
		c <- 1
		c <- 2
	}()
	println(<-c)
	println(<-c)
	<-c
}
