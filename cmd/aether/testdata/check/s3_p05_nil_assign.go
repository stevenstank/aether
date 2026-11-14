// aether:expect run

package main

// The nil check of an assignment's target comes when it is assigned, after
// the right side is evaluated, as gc orders it.

type P struct {
	x, y int
}

func g() int {
	println("g")
	return 1
}

func main() {
	var p *P
	q := &P{}
	q.y = g()
	println(q.y)
	p.y = g()
	println("unreachable")
}
