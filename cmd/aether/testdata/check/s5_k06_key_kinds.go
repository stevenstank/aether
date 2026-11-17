// aether:expect run

package main

// Keys of many kinds: NaN, which is never found and never deleted, -0 and
// +0 as one key, structs of floats, strings, and pointers, interfaces
// holding pointers, numbers of several types, and nil errors, and maps in
// interfaces (D-042).

type K struct {
	f float64
	s string
	p *int
}

func main() {
	nan := 0.0
	nan = nan / nan
	m := map[float64]int{}
	m[nan] = 1
	m[nan] = 2
	println(len(m), m[nan])
	delete(m, nan)
	println(len(m))
	x, y := 1, 2
	km := map[K]string{{1.5, "a", &x}: "one", {1.5, "a", &y}: "two", {-0.0, "", nil}: "zero"}
	println(len(km), km[K{1.5, "a", &x}], km[K{1.5, "a", &y}], km[K{0, "", nil}])
	var i any = km
	mm, ok := i.(map[K]string)
	println(ok, len(mm))
	var e error
	em := map[error]int{e: 3}
	println(em[nil])
	bm := map[bool]map[int8]uint16{true: {-1: 65535}}
	bm[false] = map[int8]uint16{}
	bm[false][3] = 7
	println(bm[true][-1], bm[false][3], len(bm[false]), len(bm))
	pm := map[*int]int{&x: 1}
	pm[&y] = 2
	println(pm[&x], pm[&y], pm[nil])
	var ia any = &x
	am := map[any]int{ia: 9, 2.5: 1, true: 2, int8(1): 3, int16(1): 4}
	println(am[&x], am[2.5], am[true], am[int8(1)], am[int16(1)], am[1])
}
