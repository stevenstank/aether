// aether:expect run

package main

// Map literals, make, reads, comma-ok reads, assignments, op=, ++, delete,
// len, clear, nil maps, growth and deletion in loops, struct, float, and
// interface keys, and maps of maps (D-042).

type P struct {
	x, y int
	s    string
}

func main() {
	m := map[string]int{"a": 1, "b": 2}
	m["c"] = 3
	m["a"] += 10
	m["b"]++
	println(len(m), m["a"], m["b"], m["c"], m["zz"])
	v, ok := m["c"]
	println(v, ok)
	v, ok = m["q"]
	println(v, ok)
	delete(m, "a")
	println(len(m), m["a"])
	var n map[int]string
	println(n == nil, len(n), n[3] == "")
	q := make(map[int]int)
	for i := 0; i < 1000; i++ {
		q[i*7] = i
	}
	s := 0
	for i := 0; i < 1000; i++ {
		s += q[i*7]
		if i%3 == 0 {
			delete(q, i*7)
		}
	}
	println(s, len(q), q[21], q[14])
	ps := map[P]P{}
	ps[P{1, 2, "x"}] = P{3, 4, "y"}
	ps[P{1, 2, "x"}] = P{5, 6, "z"}
	println(len(ps), ps[P{1, 2, "x"}].s, ps[P{1, 2, "x"}].y)
	f := map[float64]int{}
	f[0.0] = 1
	z := 0.0
	f[-z] = 2
	println(len(f), f[0])
	clear(q)
	println(len(q), q[7])
	q[7] = 70
	println(len(q), q[7])
	am := map[any]string{1: "int", "1": "str", P{1, 2, "q"}: "P", [2]int{1, 2}: "arr"}
	println(am[1], am["1"], am[P{1, 2, "q"}], am[[2]int{1, 2}], len(am))
	mm := map[string]map[string]int{}
	mm["x"] = map[string]int{"y": 5}
	mm["x"]["z"] = 6
	println(len(mm["x"]), mm["x"]["y"]+mm["x"]["z"])
}
