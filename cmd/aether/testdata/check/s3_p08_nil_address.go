// aether:expect run

package main

// &*p, &p.f, and &p[i] check p for nil.

type S struct {
	a [2]int
}

func main() {
	s := &S{}
	println(&*s == s, &s.a[1] != &s.a[0])
	var t *S
	println("before")
	p := &t.a[1]
	println(p == nil)
}
