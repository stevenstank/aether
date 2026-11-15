// aether:expect run

package main

// make, literals, indexing, len and cap, nil, copy, and append within the
// capacity, which shares the array (spec §Appending); slices as parameters,
// results, and globals.

var gs []string

func sum(s []int) int {
	t := 0
	for i := 0; i < len(s); i++ {
		t += s[i]
	}
	return t
}

func scale(s []int, k int) []int {
	for i := 0; i < len(s); i++ {
		s[i] *= k
	}
	return s[:len(s)-1]
}

func main() {
	var s []int
	println(s == nil, nil == s, len(s), cap(s), len(s[:0]), s[:0] == nil)
	t := make([]int, 2, 10)
	t[1] = 5
	t[0]++
	u := append(t, 7)
	u[0] = 9
	println(t[0], t[1], len(t), cap(t), len(u), cap(u), u[2])
	v := []int{4, 5, 6, 9: 1}
	println(len(v), cap(v), v[3], v[9], sum(v))
	w := scale(v[1:4], 10)
	println(len(w), w[0], w[1], v[1], v[3], v[4])
	n := copy(v, v[2:])
	println(n, v[0], v[1], v[7], v[8], v[9])
	println(copy(v[8:], []int{-1, -2, -3}), v[8], v[9], copy(v, s), copy(s, v))
	b := make([]uint8, 3, 3)
	b[2] = 255
	b[2] += 2
	println(b[0], b[2], len(make([]int8, 0)), make([]int, 0) == nil, []int{} == nil)
	gs = []string{"x", "yz"}
	gs[0] = gs[1]
	println(len(gs), gs[0], gs[1], len(gs[1]))
	e := make([]struct{}, 3)
	println(len(e), cap(e[1:]))
}
