// aether:expect run

package main

// An interface embedded in a struct promotes its methods through the
// struct: a call dispatches through the field's itab, and the struct's own
// method set, which its itabs hold, includes them (v2 evaluation).

type Namer interface{ Name() int }

type num int

func (n num) Name() int { return int(n) * 10 }

type T struct {
	Namer
	n int
}

type U struct {
	T
}

func (u U) Name() int { return u.T.Name() + 1 }

func main() {
	t := T{num(4), 2}
	println(t.Name(), t.n)
	var nm Namer = t
	println(nm.Name())
	nm = &t
	println(nm.Name())
	f := t.Name
	g := T.Name
	t.Namer = num(7)
	println(f(), g(t), t.Name())
	var u Namer = U{t}
	println(u.Name())
}
