// aether:expect run

package main

// The keys and values of a range, and a select case's received value and
// ok, assigned with = to interface variables, are converted to the
// variables' type, as any assignment's values are (spec §Assignability).

type P struct{ x, y int }

type N int

func (n N) Get() int { return int(n) }

type G interface{ Get() int }

func show(v any) {
	switch v := v.(type) {
	case int:
		println("int", v)
	case int8:
		println("int8", v)
	case int32:
		println("rune", v)
	case bool:
		println("bool", v)
	case string:
		println("string", v)
	case P:
		println("P", v.x, v.y)
	case N:
		println("N", v)
	case nil:
		println("nil")
	}
}

func main() {
	var k, v any
	m := map[string]P{"a": {1, 2}}
	for k, v = range m {
	}
	m["a"] = P{3, 4}
	show(k)
	show(v)
	for k, v = range [2]int8{5, 6} {
	}
	show(k)
	show(v)
	for k, v = range "hé" {
	}
	show(k)
	show(v)
	for k = range 3 {
	}
	show(k)
	var g G
	for _, g = range []N{7} {
	}
	println(g.Get())
	ch := make(chan N, 2)
	ch <- 8
	ch <- 9
	var ok any
	select {
	case v, ok = <-ch:
	}
	show(v)
	show(ok)
	select {
	case g = <-ch:
	}
	println(g.Get())
	close(ch)
	select {
	case v, ok = <-ch:
	}
	show(v)
	show(ok)
	ch2 := make(chan G, 1)
	ch2 <- N(10)
	close(ch2)
	for v = range ch2 {
	}
	show(v)
}
