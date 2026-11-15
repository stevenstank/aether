package main

// A live data structure: a complete binary tree of depth 20, 2^21 - 1
// nodes, all reachable until the end, so a collector could free nothing.

type T struct {
	l, r *T
	v    int
}

func build(d, v int) *T {
	if d == 0 {
		return &T{nil, nil, v}
	}
	return &T{build(d-1, 2*v), build(d-1, 2*v+1), v}
}

func check(t *T) int {
	if t.l == nil {
		return t.v
	}
	return t.v + check(t.l) - check(t.r)
}

func count(t *T) int {
	if t == nil {
		return 0
	}
	return 1 + count(t.l) + count(t.r)
}

func main() {
	t := build(20, 1)
	println(check(t), count(t))
}
