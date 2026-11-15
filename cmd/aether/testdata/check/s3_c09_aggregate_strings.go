// aether:expect run

package main

// == and != of arrays and structs holding strings, nested to any depth,
// compare each string by its bytes and ignore blank fields; large arrays
// are compared in a loop.

type inner struct {
	a string
	n int8
	b string
}

type outer struct {
	in [2]inner
	s  string
	_  string
	f  float64
	ss [3]string
	p  *int
}

type big struct {
	names [40]string
	tag   string
}

func mk(s string) string { return s + "" }

func main() {
	var o1, o2 outer
	println(o1 == o2, o1 != o2)
	o1.in[1].b = mk("q")
	o2.in[1].b = "q"
	println(o1 == o2)
	o1.ss[2] = "z\x00"
	println(o1 == o2, o1.in == o2.in, o1.ss != o2.ss)
	o2.ss[2] = mk("z\x00")
	println(o1 == o2)
	o1.in[0].n = -1
	println(o1 == o2, o1.in[1] == o2.in[1], o1.in[0] == o2.in[0])
	o2.in[0].n = -1
	x := 1
	o1.p, o2.p = &x, &x
	println(o1 == o2)
	var z float64
	o1.f, o2.f = z/z, z/z
	println(o1 == o2, o1.in == o2.in)
	var b1, b2 big
	for i := 0; i < 40; i++ {
		b1.names[i] = mk("n") + string([]byte{byte(97 + i)})
		b2.names[i] = string([]byte{110, byte(97 + i)})
	}
	println(b1 == b2, b1.names == b2.names)
	b2.names[39] = "n"
	println(b1 == b2, b1.names[:39][38] == b2.names[38])
	b2.names[39] = b1.names[39][:1] + b1.names[39][1:]
	b1.tag = "\xff"
	println(b1 == b2, [2]string{"a", ""} == [2]string{mk("a"), mk("")})
	type pair struct{ k, v string }
	ps := []pair{{"a", "1"}, {mk("a"), mk("1")}, {"a", "2"}}
	println(ps[0] == ps[1], ps[1] == ps[2], ps[0] != ps[2])
}
