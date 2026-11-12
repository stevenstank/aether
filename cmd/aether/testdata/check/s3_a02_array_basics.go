// aether:expect run

package main

// Array types: zero values, literals with keys and [...], indexing with
// constant and computed indices, len, nested arrays, arrays of structs and
// structs of arrays, arrays large enough to be copied in a loop, and copies
// that do not share memory.

type Cell struct {
	v    int16
	seen bool
}

type Grid struct {
	cells [3][4]Cell
	n     int
}

func main() {
	var a [5]int
	println(a[0], a[4], len(a))
	b := [5]int{1, 2, 3}
	c := [...]int{4: 10, 1: 20, 30}
	println(b[0], b[2], b[3], len(c), c[0], c[1], c[2], c[4])
	for i := 0; i < len(b); i++ {
		a[i] = b[i] * b[i]
	}
	println(a[0], a[1], a[2], a[3], a[4])
	d := a
	d[0] = -1
	println(a[0], d[0])

	var f [4]float64
	f[1] = 0.5
	f[2] = -f[1] * 3
	var u [3]uint8
	u[0] = 250
	u[0] += 10
	u[2]--
	println(f[0], f[1], f[2], u[0], u[1], u[2])

	var g Grid
	g.n = 7
	for i := 0; i < 3; i++ {
		for j := 0; j < 4; j++ {
			g.cells[i][j].v = int16(i*10 + j)
			g.cells[i][j].seen = (i+j)%2 == 0
		}
	}
	h := g
	h.cells[2][3].v = 999
	println(g.cells[2][3].v, h.cells[2][3].v, g.cells[1][2].v, g.cells[1][2].seen, h.cells[0][1].seen, h.n)
	row := g.cells[1]
	row[0].v = -5
	println(row[0].v, g.cells[1][0].v, len(row), len(g.cells))

	// The primes below 200, with a sieve.
	var composite [200]bool
	count := 0
	for i := 2; i < 200; i++ {
		if composite[i] {
			continue
		}
		count++
		for j := i * i; j < 200; j += i {
			composite[j] = true
		}
	}
	println(count)
	big := [100]int{99: 1}
	other := big
	other[50] = 2
	println(big[50], other[50], other[99])

	var s [3]string
	s[1] = "mid"
	t := [2]string{"a", "bc"}
	println(s[0], len(s[0]), s[1], t[1], len(t[1]), t[1][1])
	idx := [3]int8{-1, 0, 1}
	println(idx[idx[2]+1], b[idx[1]])
}
