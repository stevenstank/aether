package main

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
)

type pt struct{ X, Y int }

type Tree[K any, V any] struct {
	keys []K
	vals []V
}

func (t *Tree[K, V]) Put(k K, v V) { t.keys = append(t.keys, k); t.vals = append(t.vals, v) }

func (t *Tree[K, V]) All() func(yield func(K, V) bool) {
	return func(yield func(K, V) bool) {
		for i := range t.keys {
			if !yield(t.keys[i], t.vals[i]) {
				return
			}
		}
	}
}

func points(n int) iter.Seq[pt] {
	return func(yield func(pt) bool) {
		for i := 0; i < n; i++ {
			if !yield(pt{i, i * i}) {
				return
			}
		}
	}
}

func biggest(n, limit int) (pt, error) {
	for p := range points(n) {
		for q := range points(n) {
			if p.Y+q.Y > limit {
				return pt{p.X, q.X}, nil
			}
		}
	}
	return pt{}, errors.New("none")
}

func search(grid [][]int, want int) (int, int) {
	for i := 0; i < len(grid); i++ {
		for j, v := range slices.All(grid[i]) {
			switch {
			case v < 0:
				continue
			case v == want:
				return i, j
			}
		}
	}
	return -1, -1
}

func gotoOut() string {
	for v := range slices.Values([]string{"a", "b", "stop", "c"}) {
		if v == "stop" {
			goto done
		}
		fmt.Print(v)
	}
	return "never"
done:
	return "jumped"
}

func breakClassic() int {
	n := 0
outer:
	for i := 0; i < 5; i++ {
		for v := range slices.Values([]int{1, 2, 3}) {
			if i*v > 6 {
				break outer
			}
			n += v
		}
	}
	return n
}

func recovering(yield func(int) bool) {
	defer func() { fmt.Println("iterator saw:", recover()) }()
	yield(1)
}

func main() {
	fmt.Println(biggest(10, 50))
	fmt.Println(biggest(3, 50))
	fmt.Println(search([][]int{{1, -2}, {3, 4}}, 4))
	fmt.Println(search(nil, 1))
	fmt.Println(gotoOut(), breakClassic())
	t := &Tree[string, pt]{}
	t.Put("a", pt{1, 2})
	t.Put("b", pt{3, 4})
	for k, v := range t.All() {
		fmt.Print(k, v, " ")
	}
	next := t.All()
	for k := range next {
		fmt.Print(k)
	}
	fmt.Println()
	m := map[int]string{3: "c", 1: "a", 2: "b"}
	keys := slices.Collect(maps.Keys(m))
	sort.Ints(keys)
	fmt.Println(keys, len(slices.Sorted(maps.Values(m))), slices.Sorted(maps.Values(m)))
	func() {
		defer func() { fmt.Println("body panic:", recover()) }()
		for x := range recovering {
			panic(fmt.Sprint("boom", x))
		}
	}()
	var saved func(int) bool
	for x := range func(yield func(int) bool) { saved = yield; yield(5) } {
		fmt.Println("x", x)
	}
	func() {
		defer func() { fmt.Println("late:", recover()) }()
		saved(6)
	}()
}
