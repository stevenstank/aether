// aether:expect run

package main

// Methods promoted through embedded fields (D-038): of an embedded struct
// and of an embedded pointer, two levels deep, through a pointer to the
// outer struct, with a method of the outer type hiding a promoted one, and
// with pointer receivers reached by the implicit & of an embedded field.

type Base struct {
	id   int
	tag  string
	hits int
}

func (b Base) ID() int { return b.id }

func (b *Base) Hit() int {
	b.hits++
	return b.hits
}

func (b Base) Describe() string { return "base " + b.tag }

type Mid struct {
	Base
	level int
}

func (m Mid) Describe() string { return "mid " + m.tag + " over " + m.Base.Describe() }

type Top struct {
	*Mid
	extra [2]int
}

type Shared struct {
	*Base
	name string
}

func (s *Shared) Rename(n string) { s.name = n }

func main() {
	m := Mid{Base{1, "m", 0}, 2}
	println(m.ID(), m.Hit(), m.Hit(), m.Base.Hit())
	println(m.hits)
	println(m.Describe())
	println(m.Base.Describe())

	mp := &m
	println(mp.ID(), mp.Hit(), mp.Describe())

	t := Top{&m, [2]int{3, 4}}
	println(t.ID(), t.Hit(), t.Describe(), t.Mid.Base.Describe())
	tp := &t
	println(tp.Hit(), tp.Mid.Hit())
	println(m.hits)

	b := &Base{7, "shared", 0}
	s1 := Shared{b, "one"}
	s2 := Shared{b, "two"}
	s1.Hit()
	s2.Hit()
	s1.Rename("uno")
	println(b.hits, s1.ID(), s1.name, s2.name)
	println(s2.Hit(), b.hits)

	copied := m // copies Base, so its hits are its own
	copied.Hit()
	println(copied.hits, m.hits)

	arr := [2]Mid{}
	arr[1].Hit()
	arr[1].Base.Hit()
	println(arr[0].hits, arr[1].hits, arr[1].Describe())
}
