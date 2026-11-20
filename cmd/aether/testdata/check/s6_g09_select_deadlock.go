// aether:expect run

package main

// select {} in main while the only other goroutine waits on a channel
// nobody sends on: gc's deadlock error (D-048, increment 10.3).

func main() {
	c := make(chan int)
	go func() {
		select {
		case <-c:
		}
	}()
	println("waiting")
	select {}
}
