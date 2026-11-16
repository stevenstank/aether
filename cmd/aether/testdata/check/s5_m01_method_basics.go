// aether:expect run

package main

// Methods (D-038): value and pointer receivers of structs and arrays; the
// implicit & of a variable, field, element, or slice element and the
// implicit * of a pointer; receivers that are calls and literals; results
// of every kind, several of them, and of array and struct type; unnamed
// and blank receivers; a receiver whose address the method takes; methods
// in package initialization; recursion; a pointer method on nil; and a
// variable whose address escapes through the implicit & of a call.

type Counter struct {
	n    int
	name string
}

func (c *Counter) Inc() int {
	c.n++
	return c.n
}

func (c Counter) Get() int { return c.n }

func (c Counter) Bumped(k int) Counter {
	c.n += k // the receiver is a copy
	return c
}

func (c *Counter) Reset() (int, string) {
	old := c.n
	c.n = 0
	return old, c.name
}

func (c *Counter) IsNil() bool { return c == nil }

func (Counter) Kind() string { return "counter" }

func (_ *Counter) Unit() int8 { return -1 }

func (c Counter) Self() *Counter {
	return &c // a new variable each call
}

type Vec [3]float64

func (v Vec) Dot(w Vec) float64 { return v[0]*w[0] + v[1]*w[1] + v[2]*w[2] }

func (v *Vec) Scale(k float64) {
	for i := 0; i < len(v); i++ {
		v[i] *= k
	}
}

func (v Vec) Sum() float64 { return v[0] + v[1] + v[2] }

type List struct {
	val  int
	next *List
}

func (l *List) Len() int {
	if l == nil {
		return 0
	}
	return 1 + l.next.Len()
}

func (l *List) Push(v int) *List { return &List{v, l} }

func (l List) Head() int { return l.val }

type Grid struct {
	cells [2][2]Counter
	all   []Counter
}

func (g *Grid) Total() int {
	t := 0
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			t += g.cells[i][j].Get()
		}
	}
	for i := 0; i < len(g.all); i++ {
		t += g.all[i].Get()
	}
	return t
}

var global Counter
var first = global.Inc()
var second = Counter{n: 41}.Bumped(1).Get()

func newCounter(n int) Counter { return Counter{n: n, name: "made"} }

func (c *Counter) Ptr() *Counter { return c }

// escape returns the address of its own variable, taken by the implicit &
// of a method call, which therefore lives on the heap (D-035).
func escape(n int) *Counter {
	var c Counter
	c.n = n
	return c.Ptr()
}

func main() {
	println(first, second, global.Get())

	var c Counter
	c.name = "c"
	println(c.Inc(), c.Inc(), c.Get())
	d := c.Bumped(10)
	println(c.Get(), d.Get(), c.Bumped(1).Bumped(2).Get())
	old, name := c.Reset()
	println(old, name, c.Get(), c.Kind(), c.Unit())

	p := &c
	p.Inc()
	println(p.Get(), (*p).Get(), c.Get())
	(*p).Inc()
	println(c.n)

	println(newCounter(5).Get(), newCounter(6).Bumped(1).Get(), Counter{n: 9}.Kind())

	s1, s2 := c.Self(), c.Self()
	s1.Inc()
	println(s1 != s2, s1.Get(), s2.Get(), c.Get())

	var none *Counter
	println(none.IsNil(), p.IsNil(), none.Unit())

	v := Vec{1, 2, 3}
	w := v
	w.Scale(2)
	println(v.Dot(w), v.Sum(), w.Sum())
	vp := &w
	vp.Scale(0.5)
	println(vp.Dot(v), Vec{1, 1, 1}.Dot(*vp))

	var l *List
	println(l.Len())
	l = l.Push(1).Push(2).Push(3)
	println(l.Len(), l.Head(), l.next.Head(), l.next.next.Head())

	var g Grid
	g.cells[0][1].Inc()
	g.cells[1][1].Inc()
	g.cells[1][1].Inc()
	g.all = make([]Counter, 2)
	g.all[1].Inc()
	i := 0
	g.all[i].Inc()
	g.all[i].Inc()
	println(g.Total(), g.cells[1][1].Get(), g.all[0].Get())

	cs := []Counter{{n: 1}, {n: 2}}
	cs[1].Inc()
	println(cs[0].Get(), cs[1].Get(), cs[1].Bumped(cs[0].Get()).Get())
	cp := &cs[0]
	cp.Inc()
	println(cs[0].Get())

	e1 := escape(10)
	e2 := escape(20)
	e1.Inc()
	println(e1.Get(), e2.Get(), e1 != e2)

	arr := [2]Counter{}
	arr[1].Inc()
	println(arr[0].Get(), arr[1].Get())
}
