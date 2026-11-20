// aether:expect run

package main

// for range over integers, arrays, pointers to arrays, slices, and strings
// (D-049): evaluation once, copies of arrays, live reads of slices, runes
// of strings with invalid UTF-8, assignment forms, per-iteration variables,
// break and continue.

type Pt struct{ x, y int }

type Bytes []byte

type Count uint8

var calls int

func arr() [3]int {
	calls++
	return [3]int{7, 8, 9}
}

func sl() []int {
	calls++
	return []int{1, 2, 3}
}

func main() {
	for i := range 3 {
		print(i, " ")
	}
	println()
	var n int8 = 4
	for i := range n {
		print(i, " ")
	}
	for range Count(2) {
		print("x")
	}
	for i := range -2 {
		println("never", i)
	}
	println()

	a := [4]int{1, 2, 3, 4}
	for i, v := range a {
		a[3] = 100 // not seen: the range is over a copy
		print(i, ":", v, " ")
	}
	println(a[3])
	for i := range arr() {
		print(i)
	}
	for range arr() {
	}
	for _, v := range arr() {
		print(v)
	}
	println(" calls", calls)

	p := &[3]string{"a", "b", "c"}
	for i, v := range p {
		p[2] = "z" // seen: elements are read each iteration
		print(i, v, " ")
	}
	var np *[2]int
	for i := range np {
		print("nil ptr key ", i, " ")
	}
	println()

	s := []int{10, 20, 30}
	for i, v := range s {
		if i == 0 {
			s[2] = 33
			s = append(s, 40) // not seen: the length was taken once
		}
		print(v, " ")
	}
	println(len(s))
	for i := range sl() {
		print(i)
	}
	println(" calls", calls)
	pts := []Pt{{1, 2}, {3, 4}}
	for _, pt := range pts {
		pt.x = 99
	}
	for i := range pts {
		pts[i].y *= 10
	}
	println(pts[0].x, pts[0].y, pts[1].y)
	var nilS []int
	for range nilS {
		println("never")
	}

	for i, r := range "héllo, 世界\xff!" {
		print(i, ":", r, " ")
	}
	println()
	for i := range Bytes("ab") {
		print(i)
	}
	println()

	var k, v int
	for k, v = range []int{5, 6, 7} {
	}
	println(k, v)
	var keys [3]int
	j := 0
	for keys[j], v = range [3]int{11, 12, 13} {
		j++
		if j == 3 {
			break
		}
	}
	println(keys[0], keys[1], keys[2], v)

	var fs []func() int
	for i := range 3 {
		fs = append(fs, func() int { return i })
	}
	for i, x := range []int{4, 5} {
		ip := &x
		fs = append(fs, func() int { return *ip + i })
	}
	for _, f := range fs {
		print(f(), " ")
	}
	println()

	sum := 0
	for i := range 10 {
		if i%2 == 0 {
			continue
		}
		if i > 7 {
			break
		}
		sum += i
	}
	println(sum)
	defer func() { println("recovered", recover() != nil) }()
	var bad *[2]int
	for i, x := range bad {
		println(i, x)
	}
}
