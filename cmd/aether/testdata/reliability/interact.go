// Feature interactions: generic types with methods used through
// interfaces, generic worker pools over channels, method values of
// generic types run as goroutines, closures returning generic functions,
// type switches on type parameters, embedded interfaces, and recover in
// goroutines that report through channels.
package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

type Number interface {
	~int | ~int32 | ~int64 | ~float64
}

type Shape interface {
	Area() float64
	Name() string
}

type celsius float64

type Rect struct{ W, H float64 }
type Circle struct{ R float64 }
type Square struct {
	Rect
	label string
}

func (r Rect) Area() float64    { return r.W * r.H }
func (r Rect) Name() string     { return "rect" }
func (c *Circle) Area() float64 { return 3 * c.R * c.R }
func (c *Circle) Name() string  { return "circle" }
func (s Square) Name() string   { return "square:" + s.label }

type Stack[T any] struct{ items []T }

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
func (s *Stack[T]) Len() int { return len(s.items) }

type Lener interface{ Len() int }

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

func (p Pair[K, V]) String() string { return fmt.Sprintf("%v=>%v", p.Key, p.Val) }

func Sum[T Number](xs ...T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}

func Reduce[T, A any](xs []T, init A, f func(A, T) A) A {
	acc := init
	for _, x := range xs {
		acc = f(acc, x)
	}
	return acc
}

func Filter[T any](xs []T, keep func(T) bool) []T {
	var out []T
	for _, x := range xs {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

func Adder[T Number](k T) func(T) T { return func(x T) T { return x + k } }

func Compose[A, B, C any](f func(A) B, g func(B) C) func(A) C {
	return func(a A) C { return g(f(a)) }
}

func Describe[T any](v T) string {
	switch x := any(v).(type) {
	case int:
		return fmt.Sprintf("int %d", x)
	case string:
		return "string " + x
	case Shape:
		return fmt.Sprintf("shape %s %.1f", x.Name(), x.Area())
	case fmt.Stringer:
		return "stringer " + x.String()
	case nil:
		return "nil"
	}
	return fmt.Sprintf("other %T", v)
}

type Job[T any] struct {
	ID  int
	In  T
	Out chan<- Result[T]
}

type Result[T any] struct {
	ID  int
	Val T
	Err error
}

func Pool[T any](workers int, jobs []T, f func(T) T) []Result[T] {
	in := make(chan Job[T])
	out := make(chan Result[T], len(jobs))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range in {
				func() {
					defer func() {
						if r := recover(); r != nil {
							j.Out <- Result[T]{ID: j.ID, Err: fmt.Errorf("job %d: %v", j.ID, r)}
						}
					}()
					j.Out <- Result[T]{ID: j.ID, Val: f(j.In)}
				}()
			}
		}()
	}
	for i, v := range jobs {
		in <- Job[T]{i, v, out}
	}
	close(in)
	wg.Wait()
	close(out)
	res := make([]Result[T], len(jobs))
	for r := range out {
		res[r.ID] = r
	}
	return res
}

type Counter[K comparable] struct {
	mu sync.Mutex
	m  map[K]int
}

func (c *Counter[K]) Inc(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[K]int{}
	}
	c.m[k]++
}

var errOdd = errors.New("odd")

func main() {
	shapes := []Shape{Rect{2, 3}, &Circle{1}, Square{Rect{2, 2}, "s"}}
	total := Reduce(shapes, 0.0, func(a float64, s Shape) float64 { return a + s.Area() })
	names := Reduce(shapes, "", func(a string, s Shape) string { return a + s.Name() + ";" })
	fmt.Println("shapes:", total, names)
	big := Filter(shapes, func(s Shape) bool { return s.Area() > 3.5 })
	fmt.Println("big:", len(big), big[0].Name())

	var st Stack[Pair[string, []int]]
	for i, w := range strings.Fields("a bb ccc dddd") {
		st.Push(Pair[string, []int]{w, make([]int, i)})
	}
	var l Lener = &st
	fmt.Println("stack len via iface:", l.Len())
	for {
		p, ok := st.Pop()
		if !ok {
			break
		}
		fmt.Print(p, " ", len(p.Val), "; ")
	}
	fmt.Println()

	fmt.Println("sums:", Sum(1, 2, 3), Sum(1.5, 2.25), Sum[int32](), Sum([]int64{1 << 40, 1 << 40}...))
	fmt.Println("named:", Sum(celsius(1.5), celsius(2)))
	inc := Compose(Adder(10), Compose(Adder(-3), func(x int) string { return strings.Repeat("#", x) }))
	fmt.Println("compose:", inc(0))

	fmt.Println(Describe(5), "|", Describe("x"), "|", Describe(Rect{1, 1}), "|", Describe(&Circle{2}), "|", Describe(Pair[int, bool]{1, true}), "|", Describe[any](nil), "|", Describe(2.5))

	res := Pool(4, []int{1, 2, 3, 4, 5, 6, 7, 8, 0, 10}, func(x int) int {
		if x == 0 {
			panic("zero")
		}
		if x%7 == 0 {
			panic(errOdd)
		}
		return 100 / x
	})
	for _, r := range res {
		if r.Err != nil {
			fmt.Print("[", r.Err, "] ")
		} else {
			fmt.Print(r.Val, " ")
		}
	}
	fmt.Println()
	sres := Pool(3, []string{"a", "b", "c"}, strings.ToUpper)
	fmt.Println(sres[0].Val, sres[1].Val, sres[2].Val)

	var c Counter[Pair[int, string]]
	var wg sync.WaitGroup
	inc2 := c.Inc
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inc2(Pair[int, string]{i % 5, "k"})
		}()
	}
	wg.Wait()
	fmt.Println("counter:", len(c.m), c.m[Pair[int, string]{3, "k"}])
	ch := make(chan Shape, 3)
	go func() {
		defer func() { close(ch) }()
		for _, s := range shapes {
			ch <- s
		}
	}()
	for s := range ch {
		switch v := s.(type) {
		case Square:
			fmt.Print("Square ", v.label, v.W, " ")
		case *Circle:
			fmt.Print("Circle ", v.R, " ")
		default:
			fmt.Print("other ", v.Name(), " ")
		}
	}
	fmt.Println()
}
