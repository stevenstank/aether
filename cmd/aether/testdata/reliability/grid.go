// A grid world: positions and moves are structs and arrays switched on
// and used as map keys, a generic set of comparable values tracks
// visited cells, interface-typed case values match concrete tags, and an
// uncomparable value hidden in an interface field makes a comparison
// panic, which is recovered. A simulated robot walks a fixed program.
package main

import (
	"fmt"
	"sort"
)

type Pos struct{ X, Y int }

type Move [2]int

var (
	North = Move{0, -1}
	South = Move{0, 1}
	East  = Move{1, 0}
	West  = Move{-1, 0}
)

type Cell struct {
	Pos
	Kind string
}

type Set[T comparable] struct{ m map[T]struct{} }

func NewSet[T comparable](vs ...T) *Set[T] {
	s := &Set[T]{m: map[T]struct{}{}}
	s.Add(vs...)
	return s
}

func (s *Set[T]) Add(vs ...T) {
	for _, v := range vs {
		s.m[v] = struct{}{}
	}
}

func (s *Set[T]) Has(v T) bool { _, ok := s.m[v]; return ok }

func (s *Set[T]) Len() int { return len(s.m) }

func name(m Move) string {
	switch m {
	case North:
		return "N"
	case South:
		return "S"
	case East:
		return "E"
	case West:
		return "W"
	case Move{}:
		return "stay"
	}
	return fmt.Sprint(m)
}

func corner(p Pos, w, h int) string {
	switch p {
	case Pos{}:
		return "top-left"
	case Pos{w - 1, 0}:
		return "top-right"
	case Pos{0, h - 1}:
		return "bottom-left"
	case Pos{w - 1, h - 1}:
		return "bottom-right"
	}
	return ""
}

type Tagged struct {
	Label string
	Data  any
}

func same(a, b Tagged) (eq bool, err string) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Sprint(r)
		}
	}()
	switch a {
	case b:
		return true, ""
	}
	return false, ""
}

func lookup(code int, special ...any) string {
	switch code {
	case special[0]:
		return "first"
	case special[1]:
		return "second"
	}
	return "none"
}

func main() {
	const w, h = 5, 4
	walls := NewSet(Pos{2, 1}, Pos{2, 2}, Pos{3, 2})
	visited := NewSet[Pos]()
	moves := map[Move]int{}
	p := Pos{}
	program := []Move{East, East, South, South, East, North, North, East, East, South, South, South, West, Move{}, West, West}
	var trail []string
	for step, m := range program {
		next := Pos{p.X + m[0], p.Y + m[1]}
		switch {
		case next.X < 0 || next.Y < 0 || next.X >= w || next.Y >= h:
			trail = append(trail, fmt.Sprintf("%d:%s:edge", step, name(m)))
			continue
		case walls.Has(next):
			trail = append(trail, fmt.Sprintf("%d:%s:wall", step, name(m)))
			continue
		}
		p = next
		visited.Add(p)
		moves[m]++
		if c := corner(p, w, h); c != "" {
			trail = append(trail, fmt.Sprintf("%d:%s:%s", step, name(m), c))
		}
	}
	fmt.Println("end", p, "visited", visited.Len())
	fmt.Println("trail", trail)
	var names []string
	for m, n := range moves {
		names = append(names, fmt.Sprintf("%s=%d", name(m), n))
	}
	sort.Strings(names)
	fmt.Println("moves", names)

	cells := map[Cell]int{}
	for _, c := range []Cell{{Pos{1, 1}, "grass"}, {Pos{1, 1}, "grass"}, {Pos{1, 1}, "rock"}, {Pos{0, 1}, "grass"}} {
		cells[c]++
	}
	fmt.Println("cells", len(cells), cells[Cell{Pos{1, 1}, "grass"}], cells[Cell{Pos: Pos{1, 1}, Kind: "rock"}])

	fmt.Println(same(Tagged{"a", 1}, Tagged{"a", 1}))
	fmt.Println(same(Tagged{"a", 1}, Tagged{"a", int64(1)}))
	fmt.Println(same(Tagged{"a", "x"}, Tagged{"b", "x"}))
	fmt.Println(same(Tagged{"a", []int{1}}, Tagged{"a", []int{1}}))
	fmt.Println(same(Tagged{"a", map[int]int{}}, Tagged{"z", 1}))

	fmt.Println(lookup(3, 3, 4), lookup(4, int8(4), 4), lookup(5, "5", nil), lookup(6, nil, []int{}))

	var grid [h][w]byte
	for y := range grid {
		for x := range grid[y] {
			switch q := (Pos{x, y}); {
			case walls.Has(q):
				grid[y][x] = '#'
			case q == p:
				grid[y][x] = '@'
			case visited.Has(q):
				grid[y][x] = '.'
			default:
				grid[y][x] = ' '
			}
		}
		fmt.Printf("|%s|\n", grid[y][:])
	}
	switch grid {
	case [h][w]byte{}:
		fmt.Println("empty grid")
	default:
		fmt.Println("grid drawn")
	}
}
