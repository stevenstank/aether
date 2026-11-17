// aether:expect run

package main

// Methods of a type named aether whose names are those of lowering's
// generated functions (D-037, D-038), in a program that also needs every
// one of those functions: value and pointer receivers, the implicit & and
// *, promotion through an embedded aether and an embedded *aether, a nil
// pointer receiver, and a function and variables named after them too.

type aether struct {
	s string
	n int
}

func (a aether) streq(t string) bool { return a.s == t }

func (a *aether) strcmp(t string) int {
	a.n++
	if a.s < t {
		return -1
	}
	if a.s > t {
		return 1
	}
	return 0
}

func (a aether) encoderune(r rune) string { return a.s + string(r) }

func (a aether) decoderune() int { return len([]rune(a.s)) }

func (a *aether) stringtorunes() []rune {
	if a == nil {
		return nil
	}
	return []rune(a.s)
}

func (a aether) runestostring(rs []rune) string { return string(rs) + a.s }

func (a *aether) alloc() int { return a.n }

type wrap struct {
	aether
	k int
}

type wrapPtr struct {
	*aether
}

type deep struct {
	w wrap
	wrap
}

func streq(a, b string) bool { return a == b }

var main_streq = aether{s: "g"}.streq("g")

func main() {
	a := aether{s: "héllo"}
	println(main_streq, a.streq("héllo"), a.streq("hello"), streq(a.s, "x"))
	println(a.strcmp("a"), a.strcmp("z"), a.strcmp("héllo"), a.n)
	println(a.encoderune('!'), a.encoderune(0x4e16), a.decoderune())
	rs := a.stringtorunes()
	println(len(rs), a.runestostring(rs[1:3]), string(rs[0]))

	p := &a
	println(p.streq("héllo"), p.strcmp("b"), p.n, (*p).alloc(), p.alloc())

	var none *aether
	println(len(none.stringtorunes()))

	w := wrap{aether{s: "w"}, 7}
	println(w.streq("w"), w.strcmp("v"), w.n, w.aether.n, w.encoderune('x'), w.k)
	wp := &w
	wp.strcmp("w")
	println(wp.alloc(), wp.decoderune(), wp.runestostring([]rune{'y'}))

	q := wrapPtr{&a}
	println(q.streq("héllo"), q.strcmp("h"), q.alloc(), a.n, q.decoderune())

	var d deep
	d.w.s = "inner"
	d.s = "outer"
	println(d.streq("outer"), d.w.streq("inner"), d.strcmp("o"), d.w.strcmp("j"), d.n, d.w.n)

	var e wrapPtr
	println(e.alloc())
}
