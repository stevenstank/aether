// aether:expect run

package main

// A compound assignment to a map element whose division or remainder
// panics leaves the map unchanged: the element is inserted only after the
// operation, as in gc (go.dev/issue/22881). gc inserts it before a shift's
// panic. Without a panic, a missing element is read as zero and inserted.

func try(f func(m map[int]int), m map[int]int) {
	defer func() {
		println("recovered", recover() != nil, len(m))
	}()
	f(m)
}

func main() {
	var z int
	var s uint = 70
	n := -1
	m := map[int]int{}
	try(func(m map[int]int) { m[0] /= z }, m)
	try(func(m map[int]int) { m[1] %= z }, m)
	try(func(m map[int]int) { m[2] <<= n }, m)
	try(func(m map[int]int) { m[3] += 5 }, m)
	m[3] *= 3
	m[4] <<= s
	m[5] -= 2
	println(m[3], m[4], m[5], len(m))
	var nilMap map[int]int
	try(func(m map[int]int) { m[0] /= z }, nilMap)
	try(func(m map[int]int) { m[0] += 1 }, nilMap)
}
