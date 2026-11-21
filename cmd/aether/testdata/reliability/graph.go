// Graph algorithms over generic containers: a binary heap, Dijkstra,
// breadth-first search, topological sort, and connected components with
// a union-find.
package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

type Heap[T any] struct {
	items []T
	less  func(a, b T) bool
}

func (h *Heap[T]) Len() int { return len(h.items) }

func (h *Heap[T]) Push(x T) {
	h.items = append(h.items, x)
	i := len(h.items) - 1
	for i > 0 {
		p := (i - 1) / 2
		if !h.less(h.items[i], h.items[p]) {
			break
		}
		h.items[i], h.items[p] = h.items[p], h.items[i]
		i = p
	}
}

func (h *Heap[T]) Pop() T {
	top := h.items[0]
	n := len(h.items) - 1
	h.items[0] = h.items[n]
	h.items = h.items[:n]
	i := 0
	for {
		l, r, m := 2*i+1, 2*i+2, i
		if l < n && h.less(h.items[l], h.items[m]) {
			m = l
		}
		if r < n && h.less(h.items[r], h.items[m]) {
			m = r
		}
		if m == i {
			return top
		}
		h.items[i], h.items[m] = h.items[m], h.items[i]
		i = m
	}
}

type Edge struct {
	to, w int
}

type Graph struct {
	n   int
	adj [][]Edge
}

func NewGraph(n int) *Graph { return &Graph{n: n, adj: make([][]Edge, n)} }

func (g *Graph) Add(a, b, w int) {
	g.adj[a] = append(g.adj[a], Edge{b, w})
}

type item struct{ node, dist int }

func (g *Graph) Dijkstra(src int) ([]int, []int) {
	const inf = 1 << 60
	dist := make([]int, g.n)
	prev := make([]int, g.n)
	for i := range dist {
		dist[i], prev[i] = inf, -1
	}
	dist[src] = 0
	h := &Heap[item]{less: func(a, b item) bool {
		if a.dist != b.dist {
			return a.dist < b.dist
		}
		return a.node < b.node
	}}
	h.Push(item{src, 0})
	for h.Len() > 0 {
		it := h.Pop()
		if it.dist > dist[it.node] {
			continue
		}
		for _, e := range g.adj[it.node] {
			if d := it.dist + e.w; d < dist[e.to] {
				dist[e.to], prev[e.to] = d, it.node
				h.Push(item{e.to, d})
			}
		}
	}
	return dist, prev
}

func path(prev []int, to int) []int {
	var p []int
	for ; to >= 0; to = prev[to] {
		p = append(p, to)
	}
	slices.Reverse(p)
	return p
}

func (g *Graph) BFS(src int) []int {
	level := make([]int, g.n)
	for i := range level {
		level[i] = -1
	}
	level[src] = 0
	q := []int{src}
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		for _, e := range g.adj[u] {
			if level[e.to] < 0 {
				level[e.to] = level[u] + 1
				q = append(q, e.to)
			}
		}
	}
	return level
}

func (g *Graph) Topo() ([]int, bool) {
	indeg := make([]int, g.n)
	for _, es := range g.adj {
		for _, e := range es {
			indeg[e.to]++
		}
	}
	h := &Heap[int]{less: func(a, b int) bool { return a < b }}
	for i, d := range indeg {
		if d == 0 {
			h.Push(i)
		}
	}
	var order []int
	for h.Len() > 0 {
		u := h.Pop()
		order = append(order, u)
		for _, e := range g.adj[u] {
			if indeg[e.to]--; indeg[e.to] == 0 {
				h.Push(e.to)
			}
		}
	}
	return order, len(order) == g.n
}

type UF struct{ parent, rank []int }

func NewUF(n int) *UF {
	u := &UF{make([]int, n), make([]int, n)}
	for i := range u.parent {
		u.parent[i] = i
	}
	return u
}

func (u *UF) Find(x int) int {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

func (u *UF) Union(a, b int) bool {
	a, b = u.Find(a), u.Find(b)
	if a == b {
		return false
	}
	if u.rank[a] < u.rank[b] {
		a, b = b, a
	}
	u.parent[b] = a
	if u.rank[a] == u.rank[b] {
		u.rank[a]++
	}
	return true
}

func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func MaxBy[T any, K cmp.Ordered](xs []T, key func(T) K) (T, K) {
	best, bk := xs[0], key(xs[0])
	for _, x := range xs[1:] {
		if k := key(x); k > bk {
			best, bk = x, k
		}
	}
	return best, bk
}

func main() {
	const n = 60
	g := NewGraph(n)
	seed := uint32(7)
	next := func() int {
		seed = seed*1664525 + 1013904223
		return int(seed >> 8)
	}
	for i := range n {
		for k := 0; k < 3; k++ {
			j := next() % n
			if j > i {
				g.Add(i, j, next()%20+1)
			}
		}
		if i+1 < n {
			g.Add(i, i+1, 25)
		}
	}
	dist, prev := g.Dijkstra(0)
	fmt.Println("dist to 59:", dist[n-1], "path:", path(prev, n-1))
	idx := 0
	far, d := MaxBy(Map(make([]int, n), func(int) item { idx++; return item{idx - 1, dist[idx-1]} }), func(it item) int { return it.dist })
	fmt.Println("farthest:", far.node, d)
	lv := g.BFS(0)
	hist := map[int]int{}
	for _, l := range lv {
		hist[l]++
	}
	var keys []int
	for k := range hist {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "%d:%d ", k, hist[k])
	}
	fmt.Println("bfs levels:", sb.String())
	order, ok := g.Topo()
	fmt.Println("topo ok:", ok, "first:", order[:10], "last:", order[len(order)-5:])
	g.Add(n-1, 0, 1)
	_, ok = g.Topo()
	fmt.Println("topo after cycle:", ok)
	uf := NewUF(n)
	comps := n
	for i := 0; i < 40; i++ {
		if uf.Union(next()%n, next()%n) {
			comps--
		}
	}
	sizes := map[int]int{}
	for i := range n {
		sizes[uf.Find(i)]++
	}
	sz := make([]int, 0, len(sizes))
	for _, v := range sizes {
		sz = append(sz, v)
	}
	slices.SortFunc(sz, func(a, b int) int { return cmp.Compare(b, a) })
	fmt.Println("components:", comps, len(sizes), "largest:", sz[:min(5, len(sz))])
	words := Map([]int{3, 1, 4, 1, 5, 9, 2, 6}, func(i int) string { return strings.Repeat("*", i) })
	fmt.Println(strings.Join(words, "|"))
}
