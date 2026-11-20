// aether:expect run

package main

// Local defined types of non-aggregate underlying types (D-049): two local
// types of the same name in different functions are different types, and
// a panic writes the name gc gives them.

func f() any {
	type T int
	return T(1)
}

func g() any {
	type T int
	return T(1)
}

func main() {
	type P *int
	type S []string
	type M map[string]P
	x := 1
	var p P = &x
	*p = 2
	s := S{"a", "b"}
	m := M{"x": p}
	println(*p, len(s), s[1], *m["x"])
	a, b := f(), g()
	println(a == b, a == f())
	type E string
	panic(E("local"))
}
