// aether:expect unsupported generic-instance

package main

// In the copy for T = P, the conversion T(v) would become P(v), a call of
// the local func P, which has the type P too: the copy type-checks.

type P struct{ a int }

func conv[T ~struct{ a int }](v struct{ a int }) T {
	P := func(s struct{ a int }) T { return T{s.a + 1} }
	if P != nil {
		return T(v)
	}
	return P(v)
}

func main() {
	println(conv[P](struct{ a int }{1}).a)
}
