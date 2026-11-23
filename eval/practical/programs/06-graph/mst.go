package main

import (
	"errors"
	"sort"
)

// UnionFind is a disjoint-set forest with path compression and union by
// rank.
type UnionFind struct {
	parent, rank []int
	sets         int
}

func NewUnionFind(n int) *UnionFind {
	u := &UnionFind{parent: make([]int, n), rank: make([]int, n), sets: n}
	for i := range u.parent {
		u.parent[i] = i
	}
	return u
}

func (u *UnionFind) Find(x int) int {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

func (u *UnionFind) Union(a, b int) bool {
	ra, rb := u.Find(a), u.Find(b)
	if ra == rb {
		return false
	}
	switch {
	case u.rank[ra] < u.rank[rb]:
		ra, rb = rb, ra
	case u.rank[ra] == u.rank[rb]:
		u.rank[ra]++
	}
	u.parent[rb] = ra
	u.sets--
	return true
}

type wedge struct {
	u, v int
	w    int64
}

// MST returns a minimum spanning forest (Kruskal), its total weight, and the
// number of trees in the forest.
func MST(g *Graph) (edges []wedge, total int64, trees int, err error) {
	if g.Directed {
		return nil, 0, 0, errors.New("spanning tree needs an undirected graph")
	}
	var all []wedge
	for u := range g.adj {
		for _, e := range g.adj[u] {
			if u <= e.To {
				all = append(all, wedge{u, e.To, e.Weight})
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].w != all[j].w {
			return all[i].w < all[j].w
		}
		a, b := all[i], all[j]
		if g.Name(a.u) != g.Name(b.u) {
			return g.Name(a.u) < g.Name(b.u)
		}
		return g.Name(a.v) < g.Name(b.v)
	})
	uf := NewUnionFind(g.Len())
	for _, e := range all {
		if uf.Union(e.u, e.v) {
			edges = append(edges, e)
			total += e.w
		}
	}
	return edges, total, uf.sets, nil
}
