// aether:expect run

package main

// Tuple assignments of arrays, structs, and their elements: every value is
// evaluated before any is assigned, so they swap.

type T struct {
	a, b int
	s    [2]bool
}

func main() {
	x := [2]int{1, 2}
	y := [2]int{3, 4}
	x, y = y, x
	println(x[0], x[1], y[0], y[1])
	x[0], x[1] = x[1], x[0]
	println(x[0], x[1])
	t := T{a: 1, b: 2, s: [2]bool{true, false}}
	t.a, t.b = t.b, t.a
	t.s[0], t.s[1] = t.s[1], t.s[0]
	println(t.a, t.b, t.s[0], t.s[1])
	u := T{a: 9}
	t, u = u, t
	println(t.a, t.b, t.s[1], u.a, u.b, u.s[1])
	i := 0
	var z [3]int
	i, z[i] = 2, 5
	println(i, z[0], z[2])
	var big1, big2 [30]int
	big1[29] = 1
	big1, big2 = big2, big1
	println(big1[29], big2[29])
}
