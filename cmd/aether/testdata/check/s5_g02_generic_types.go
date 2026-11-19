// aether:expect run

package main

// Generic types (D-045): structs, arrays, and interfaces with type
// parameters, methods with value and pointer receivers, recursive types,
// instances of instances, instances embedded in structs with their
// methods promoted, method values and expressions of instances, maps of
// instances, and the gc names of instances in recovered panics.

type Stack[T any] struct {
	items []T
}

func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }

func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v, true
}

func (s Stack[T]) Len() int { return len(s.items) }

type List[T any] struct {
	head T
	tail *List[T]
}

func Cons[T any](v T, l *List[T]) *List[T] { return &List[T]{v, l} }

func (l *List[T]) Len() int {
	if l == nil {
		return 0
	}
	return 1 + l.tail.Len()
}

func (l *List[T]) Reverse() *List[T] {
	var r *List[T]
	for ; l != nil; l = l.tail {
		r = Cons(l.head, r)
	}
	return r
}

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

func MakePair[K comparable, V any](k K, v V) Pair[K, V] { return Pair[K, V]{k, v} }

func (p Pair[K, V]) Swap() Pair[string, K] { return Pair[string, K]{"swapped", p.Key} }

type Vec[T any] [3]T

func (v *Vec[T]) Set(i int, x T) { v[i] = x }

func (v Vec[T]) Get(i int) T { return v[i] }

type Getter[T any] interface{ Get(int) T }

type Named[T any] struct {
	Stack[T]
	name string
}

type Cache[K comparable, V any] struct {
	m     map[K]V
	hits  int
	total int
}

func NewCache[K comparable, V any]() *Cache[K, V] { return &Cache[K, V]{m: map[K]V{}} }

func (c *Cache[K, V]) Get(k K, make func(K) V) V {
	c.total++
	if v, ok := c.m[k]; ok {
		c.hits++
		return v
	}
	v := make(k)
	c.m[k] = v
	return v
}

type P struct{ x int }

func name(f func()) {
	defer func() {
		e := recover()
		if err, ok := e.(error); ok {
			println(err.Error())
		}
	}()
	f()
}

func main() {
	var s Stack[int]
	s.Push(1)
	s.Push(2)
	v, ok := s.Pop()
	println(v, ok, s.Len())
	ps := &Stack[P]{}
	ps.Push(P{3})
	q, ok := ps.Pop()
	q2, ok2 := ps.Pop()
	println(q.x, ok, q2.x, ok2)

	var l *List[string]
	for i := 0; i < 4; i++ {
		l = Cons(string(rune('a'+i)), l)
	}
	r := l.Reverse()
	println(l.Len(), l.head, r.head, r.tail.head)

	p := MakePair("k", 3.5)
	sw := p.Swap()
	println(p.Key, p.Val, sw.Key, sw.Val)
	pp := MakePair(p, Pair[int, bool]{1, true})
	println(pp.Key.Val, pp.Val.Val)

	var vec Vec[float64]
	vec.Set(1, 2.5)
	var g Getter[float64] = vec
	println(g.Get(1), vec.Get(0))
	get := vec.Get
	set := (*Vec[float64]).Set
	set(&vec, 2, -1)
	println(get(1), vec[2], len(vec))

	n := Named[string]{name: "n"}
	n.Push("x")
	n.Push("y")
	top, ok := n.Pop()
	println(n.name, top, ok, n.Len())

	c := NewCache[int, string]()
	sq := func(k int) string { return string(rune('0' + k*k%10)) }
	println(c.Get(3, sq), c.Get(4, sq), c.Get(3, sq), c.hits, c.total, len(c.m))

	m := map[Pair[int, int]]Stack[int]{}
	m[Pair[int, int]{1, 2}] = s
	println(len(m), m[Pair[int, int]{1, 2}].Len(), m[Pair[int, int]{2, 1}].Len())

	var e any = pp
	name(func() { println(e.(int)) })
	e = &s
	name(func() { println(e.(Getter[int]).Get(0)) })
	e = vec
	name(func() { println(e.(Vec[int])[0]) })
	e = List[*P]{}
	name(func() { println(e.(int)) })
	e = Stack[[]map[string]P]{}
	name(func() { println(e.(int)) })
	e = Pair[struct{ a, B int }, func(int) bool]{}
	name(func() { println(e.(int)) })
	e = Pair[interface{ Get(int) byte }, error]{}
	name(func() { println(e.(int)) })
}
