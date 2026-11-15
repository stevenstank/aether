// aether:expect run

package main

// append that grows: element values, lengths, and that a grown slice keeps
// its elements, for several element types, spread arguments, a slice
// appended to itself, and nested slices. Capacities after growth are not
// printed: Aether's growth rule differs from gc's (V-6).

type P struct {
	x int8
	s string
	f float64
}

func push(s []P, n int) []P {
	for i := 0; i < n; i++ {
		s = append(s, P{int8(i), "p", float64(i) / 2})
	}
	return s
}

func main() {
	var s []int
	for i := 0; i < 1000; i++ {
		s = append(s, i*i)
	}
	t := 0
	for i := 0; i < len(s); i++ {
		t += s[i]
	}
	println(len(s), s[999], t)
	s = append(s[:2], s[998:]...)
	println(len(s), s[2], s[3])
	s = append(s, s...)
	s = append(s, s[1:3]...)
	println(len(s), s[4], s[7], s[9])
	ps := push(nil, 37)
	println(len(ps), ps[36].x, ps[36].s, ps[35].f)
	var nest [][]bool
	for i := 0; i < 5; i++ {
		row := make([]bool, i)
		nest = append(nest, append(row, true))
	}
	println(len(nest), len(nest[4]), nest[4][4], nest[3][0])
	var z []struct{}
	for k := 0; k < 70; k++ {
		z = append(z, struct{}{})
	}
	var e []int
	e = append(e)
	e = append(e, []int(nil)...)
	println(len(z), e == nil, len(append([]byte(nil), 1, 2, 3)))
}
