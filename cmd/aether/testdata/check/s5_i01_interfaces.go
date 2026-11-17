// aether:expect run

package main

// Interfaces (D-041): declarations, embedding, any and error; implicit and
// explicit conversions of values of every kind, in assignments, arguments,
// results, fields, elements, and package-level variables; method calls
// with value and pointer receivers, promoted through embedded structs;
// interface to interface conversions; method values and expressions of
// interfaces; comparisons, of structs holding interfaces too; and a nil
// pointer in an interface, which is not nil.

type Named interface{ Name() string }

type Sizer interface{ Size() int }

type Thing interface {
	Named
	Sizer
}

type Box struct {
	w, h int
	tag  string
}

func (b Box) Name() string { return "box:" + b.tag }
func (b Box) Size() int     { return b.w * b.h }

// Grow changes its own copy of the receiver.
func (b Box) Grow() int {
	b.w++
	return b.w
}

type Grower interface{ Grow() int }

type Counter struct{ n int }

func (c *Counter) Name() string { return "counter" }
func (c *Counter) Size() int {
	if c == nil {
		return -1
	}
	c.n++
	return c.n
}

type Labeled struct {
	Box
	extra int
}

type MyErr struct{ code int }

func (e *MyErr) Error() string { return "code " + string(rune('0'+e.code)) }

type Pair struct {
	a, b any
}

func describe(t Thing) string { return t.Name() }

func sum(ss []Sizer) int {
	n := 0
	for i := 0; i < len(ss); i++ {
		n += ss[i].Size()
	}
	return n
}

func find(ok bool) error {
	if ok {
		return nil
	}
	return &MyErr{7}
}

func wrap(x any) any { return x }

var global Thing = Box{2, 2, "g"}
var anyGlobal any = 3.5

func main() {
	var t Thing = Box{3, 4, "a"}
	println(t.Name(), t.Size(), describe(t), global.Size())
	c := &Counter{}
	t = c
	println(t.Size(), t.Size(), c.n, describe(Thing(c)))
	var n Named = t // to an interface of fewer methods
	var s Sizer = Labeled{Box{5, 5, "l"}, 1}
	println(n.Name(), s.Size(), sum([]Sizer{s, c, Box{1, 2, ""}}), c.n)

	// Every kind of value in an any.
	vals := []any{1, int8(-2), uint16(3), 2.5, float32(1.25), "str", true, []int{1}, [2]int{4, 5},
		Box{1, 1, "x"}, c, nil, wrap(9), anyGlobal}
	println(len(vals), vals[0] == 1, vals[1] == int8(-2), vals[1] == -2, vals[3] == 2.5, vals[5] == "str")
	println(vals[6] == true, vals[8] == [2]int{4, 5}, vals[9] == Box{1, 1, "x"}, vals[10] == any(c), vals[11] == nil)
	println(vals[12] == 9, vals[13] == 3.5, vals[0] == vals[12], vals[2] == uint16(3))

	// Errors.
	e1, e2 := find(true), find(false)
	println(e1 == nil, e2 != nil, e2.Error())
	var me *MyErr
	var e3 error = me
	println(me == nil, e3 != nil)

	// A nil pointer in an interface: its method is called on nil.
	var nc *Counter
	var sz Sizer = nc
	println(sz != nil, sz.Size())

	// Method values and expressions.
	f := t.Name
	g := Thing.Size
	h := Sizer.Size
	println(f(), g(t), h(c), c.n)

	// Comparisons: the same dynamic type and value, or not.
	p1 := Pair{1, "a"}
	p2 := Pair{1, "a"}
	p3 := Pair{1, "b"}
	println(p1 == p2, p1 == p3, p1 != p3)
	var a1, a2 any = Box{1, 2, "z"}, Box{1, 2, "z"}
	println(a1 == a2, a1 != any(Box{1, 2, "y"}), any(1) == any(int64(1)), any(nil) == nil)
	var t1, t2 Thing = c, c
	println(t1 == t2, t1 == Thing(&Counter{}), Named(t1) == t2)

	// A value method changes its copy, never the interface's.
	var gr Grower = Box{1, 1, "g"}
	println(gr.Grow(), gr.Grow(), gr.(Box).w)

	// Interface to interface, and back.
	var x any = t
	back := x.(Thing)
	println(back.Name(), x.(Named).Name())
}
