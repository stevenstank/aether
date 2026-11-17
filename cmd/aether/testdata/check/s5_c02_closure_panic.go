// aether:expect run

package main

// A panic in a closure, called through another (D-040): the output before
// it, then gc's message, after the captured slice was changed by an
// earlier call.

var log = func(s string) { println("log", s) }

func main() {
	xs := []int{1, 2, 3}
	at := func(i int) int { return xs[i] }
	shrink := func() { xs = xs[:1] }
	run := func(f func(int) int, i int) int {
		log("run")
		return f(i)
	}
	println(run(at, 2))
	shrink()
	println(len(xs), run(at, 0))
	println(run(at, 2))
	println("not reached")
}
