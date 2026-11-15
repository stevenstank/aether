package main

// Garbage: each round builds a list of n nodes, sums it, and drops it, so
// only one list is ever live. Aether never frees, so its heap grows by every
// round's nodes; TestBenchHeap varies rounds to measure that growth.

type Node struct {
	v    int
	next *Node
}

const n = 100_000

const rounds = 64

func build(k int) *Node {
	var h *Node
	for i := 0; i < n; i++ {
		h = &Node{i ^ k, h}
	}
	return h
}

func sum(h *Node) int {
	s := 0
	for ; h != nil; h = h.next {
		s += h.v
	}
	return s
}

func main() {
	total := 0
	for r := 0; r < rounds; r++ {
		total += sum(build(r))
	}
	println(total)
}
