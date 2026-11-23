package main

import (
	"fmt"
	"slices"
	"strings"
)

// CycleError reports a cycle found while sorting.
type CycleError struct{ Cycle []string }

func (e *CycleError) Error() string {
	return "graph has a cycle: " + strings.Join(e.Cycle, " -> ")
}

// TopoSort orders the nodes of a directed graph so that every edge goes
// forward. Among ready nodes the one with the smallest name comes first.
func TopoSort(g *Graph) ([]int, error) {
	if !g.Directed {
		return nil, fmt.Errorf("topological sort needs a directed graph")
	}
	indeg := make([]int, g.Len())
	for u := range g.adj {
		for _, e := range g.adj[u] {
			indeg[e.To]++
		}
	}
	h := NewNameHeap(g)
	for v, d := range indeg {
		if d == 0 {
			h.push(v)
		}
	}
	var out []int
	for h.len() > 0 {
		u := h.pop()
		out = append(out, u)
		for _, e := range g.adj[u] {
			indeg[e.To]--
			if indeg[e.To] == 0 {
				h.push(e.To)
			}
		}
	}
	if len(out) != g.Len() {
		return nil, &CycleError{findCycle(g, indeg)}
	}
	return out, nil
}

// findCycle walks from a node left with positive in-degree until a node
// repeats; every such node lies on or leads into a cycle.
func findCycle(g *Graph, indeg []int) []string {
	start := -1
	for v, d := range indeg {
		if d > 0 && (start < 0 || g.Name(v) < g.Name(start)) {
			start = v
		}
	}
	seen := map[int]int{}
	var walk []int
	v := start
	for {
		if i, ok := seen[v]; ok {
			cyc := append([]int{}, walk[i:]...)
			cyc = append(cyc, v)
			names := make([]string, len(cyc))
			for k, id := range cyc {
				names[k] = g.Name(id)
			}
			return names
		}
		seen[v] = len(walk)
		walk = append(walk, v)
		next := -1
		for _, e := range g.adj[v] {
			if indeg[e.To] > 0 && (next < 0 || g.Name(e.To) < g.Name(next)) {
				next = e.To
			}
		}
		v = next
	}
}

// nameHeap is a min-heap of node ids ordered by name.
type nameHeap struct {
	g   *Graph
	ids []int
}

func NewNameHeap(g *Graph) *nameHeap { return &nameHeap{g: g} }
func (h *nameHeap) len() int         { return len(h.ids) }
func (h *nameHeap) less(a, b int) bool {
	return h.g.Name(h.ids[a]) < h.g.Name(h.ids[b])
}
func (h *nameHeap) push(v int) {
	h.ids = append(h.ids, v)
	for i := len(h.ids) - 1; i > 0; {
		p := (i - 1) / 2
		if !h.less(i, p) {
			break
		}
		h.ids[i], h.ids[p] = h.ids[p], h.ids[i]
		i = p
	}
}
func (h *nameHeap) pop() int {
	top := h.ids[0]
	n := len(h.ids) - 1
	h.ids[0] = h.ids[n]
	h.ids = h.ids[:n]
	for i := 0; ; {
		l, r, m := 2*i+1, 2*i+2, i
		if l < n && h.less(l, m) {
			m = l
		}
		if r < n && h.less(r, m) {
			m = r
		}
		if m == i {
			break
		}
		h.ids[i], h.ids[m] = h.ids[m], h.ids[i]
		i = m
	}
	return top
}

// SCC returns the strongly connected components (Tarjan's algorithm).
// Each component is sorted by name, and components are sorted by their
// first name.
func SCC(g *Graph) [][]string {
	index := make([]int, g.Len())
	low := make([]int, g.Len())
	onStack := make([]bool, g.Len())
	for i := range index {
		index[i] = -1
	}
	var stack []int
	var comps [][]string
	next := 0
	var strong func(v int)
	strong = func(v int) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, e := range g.adj[v] {
			w := e.To
			if index[w] < 0 {
				strong(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var comp []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			comps = append(comps, g.SortedNames(comp))
		}
	}
	for v := 0; v < g.Len(); v++ {
		if index[v] < 0 {
			strong(v)
		}
	}
	slices.SortFunc(comps, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	return comps
}
