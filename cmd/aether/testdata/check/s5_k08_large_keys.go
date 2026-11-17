// aether:expect run

package main

// Keys of large arrays and structs, hashed and compared by loops, and of
// arrays of interfaces; deleting the last entry makes the map empty again,
// so an unhashable key gets the key-check message (D-042).

type Big struct {
	a [20]int
	s [3]string
	_ int
	f float32
}

func main() {
	m := map[[20]int]int{}
	var k [20]int
	for i := 0; i < 100; i++ {
		k[i%20] += i
		m[k] += i
	}
	println(len(m), m[k], m[[20]int{}])
	bm := map[Big][]Big{}
	var b Big
	b.s[2] = "z"
	b.f = -0.0
	bm[b] = append(bm[b], b)
	f := float32(0)
	b.f = -f
	bm[b] = append(bm[b], b)
	println(len(bm), len(bm[b]), bm[Big{s: [3]string{2: "z"}}][1].s[2])
	am := map[[2]any]int{{1, 2}: 1}
	delete(am, [2]any{1, 2})
	var u any = []string{}
	println(len(am))
	v, ok := am[[2]any{3, u}]
	println(v, ok)
}
