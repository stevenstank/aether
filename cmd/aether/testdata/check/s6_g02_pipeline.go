// aether:expect run

package main

// Many goroutines: a pipeline of stages, a fan-out and fan-in whose
// results are summed, a ping-pong, and goroutines blocked on channels
// nobody uses again when main returns (D-048).

func gen(n int) <-chan int {
	out := make(chan int)
	go func() {
		for i := 1; i <= n; i++ {
			out <- i
		}
		close(out)
	}()
	return out
}

func square(in <-chan int) <-chan int {
	out := make(chan int, 4)
	go func() {
		for v := range in {
			out <- v * v
		}
		close(out)
	}()
	return out
}

func main() {
	s := 0
	for v := range square(square(gen(20))) {
		s += v
	}
	println(s)

	results := make(chan int)
	const workers = 1000
	for i := 0; i < workers; i++ {
		go func(i int) { results <- i * 2 }(i)
	}
	total := 0
	for i := 0; i < workers; i++ {
		total += <-results
	}
	println(total)

	ping, pong := make(chan int), make(chan int)
	go func() {
		for v := range ping {
			pong <- v + 1
		}
		close(pong)
	}()
	v := 0
	for i := 0; i < 10000; i++ {
		ping <- v
		v = <-pong
	}
	close(ping)
	z, ok := <-pong
	println(v, z, ok)

	stuck := make(chan int)
	for i := 0; i < 10; i++ {
		go func() { <-stuck }()
	}
	println("done")
}
