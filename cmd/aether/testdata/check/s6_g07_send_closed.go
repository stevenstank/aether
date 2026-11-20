// aether:expect run

package main

// A send on a closed channel in a program without defer statements: the
// panic ends the program at once (D-048).

func main() {
	c := make(chan int, 1)
	c <- 1
	close(c)
	println(<-c)
	println(<-c)
	go func() { c <- 2 }()
	<-make(chan int)
}
