// A program instantiating other packages' generic code, of its own module
// and of the standard library, with its own types: instances that main
// and shape host (D-052).
package main

import (
	"errors"
	"fmt"
	"slices"
	"sync/atomic"

	"example.com/pk/gen"
	"example.com/pk/shape"
)

type point struct{ x, y int }

type words []string

type codeErr struct{ code int }

func (e *codeErr) Error() string { return fmt.Sprintf("code %d", e.code) }

// holder embeds an instance main hosts.
type holder struct {
	gen.Stack[point]
	n int
}

func main() {
	var s gen.Stack[point]
	s.Push(point{1, 2})
	s.Push(point{3, 4})
	p, ok := s.Pop()
	fmt.Printf("%T %v %v %d\n", s, p, ok, s.Len())
	fmt.Printf("%+v %v\n", s, &s)
	var l interface{ Len() int } = &s
	var a any = s
	_, isStack := a.(gen.Stack[point])
	_, isInts := a.(gen.Stack[int])
	fmt.Println(l.Len(), isStack, isInts)
	h := holder{n: 1}
	h.Push(point{5, 6})
	fmt.Println(h.Len(), h.Stack.Len(), h.n)

	ps := shape.Stack(shape.Point{X: 1, Y: 1}, shape.Point{X: 2, Y: 2})
	ps.Push(shape.Point{X: 3, Y: 3})
	q, _ := ps.Pop()
	var mine gen.Stack[shape.Point]
	mine.Push(q)
	fmt.Printf("%T %v %d %d\n", ps, q, ps.Len(), mine.Len())
	fmt.Println(shape.Tags([]int{0, 2}))
	fmt.Println(gen.Map([]point{{1, 2}, {3, 4}}, func(p point) int { return p.x * p.y }))

	pts := []point{{3, 0}, {1, 0}, {2, 0}}
	slices.SortFunc(pts, func(a, b point) int { return a.x - b.x })
	fmt.Println(pts, slices.Index(pts, point{2, 0}), slices.Contains(pts, point{9, 9}))
	ws := words{"ccc", "a", "bb", "dd"}
	slices.SortStableFunc(ws, func(a, b string) int { return len(a) - len(b) })
	fmt.Printf("%T %v %d\n", ws, ws, slices.IndexFunc(ws, func(s string) bool { return s == "dd" }))

	var ap atomic.Pointer[point]
	fmt.Println(ap.Load() == nil)
	ap.Store(&pts[0])
	fmt.Printf("%T %v\n", &ap, *ap.Load())

	err := fmt.Errorf("wrapped: %w", &codeErr{7})
	if e, ok := errors.AsType[*codeErr](err); ok {
		fmt.Println(e.code, e)
	}
	_, ok = errors.AsType[*codeErr](errors.New("plain"))
	fmt.Println(ok)
}
