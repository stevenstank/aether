// aether:expect run

package main

// A panic runs the deferred calls of every frame it passes through, in
// order, then writes its value and exits with status 2 (D-043).

func leaf(n int) int {
	defer println("leaf deferred", n)
	if n == 3 {
		panic("leaf failed")
	}
	return n
}

func middle() (int, string) {
	defer func() { println("middle deferred") }()
	t := 0
	for i := 1; i <= 5; i++ {
		t += leaf(i)
		println("total", t)
	}
	return t, "unreached"
}

func main() {
	defer println("main deferred")
	x, s := middle()
	println("unreached", x, s)
}
