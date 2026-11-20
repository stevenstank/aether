// aether:expect run

package main

// A panic in a goroutine runs that goroutine's deferred calls and ends the
// program; main is blocked meanwhile (D-048).

type E struct{ n int }

func (e E) Error() string { return "E" }

func main() {
	c := make(chan int)
	defer println("main's deferred call does not run")
	go func() {
		defer println("deferred in goroutine")
		defer func() {
			r := recover()
			println("recovered", r.(string))
			panic(E{c2(c)})
		}()
		panic("first")
	}()
	<-c
}

func c2(c chan int) int { return cap(c) }
