package main

import (
	"container/heap"
	"errors"
	"fmt"
	"math"
)

var ErrNegativeWeight = errors.New("negative edge weight")

type item struct {
	node int
	dist int64
}

type pq []item

func (q pq) Len() int { return len(q) }
func (q pq) Less(i, j int) bool {
	if q[i].dist != q[j].dist {
		return q[i].dist < q[j].dist
	}
	return q[i].node < q[j].node
}
func (q pq) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *pq) Push(x any)   { *q = append(*q, x.(item)) }
func (q *pq) Pop() any {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}

const Inf = math.MaxInt64

// Dijkstra returns shortest distances from src and each node's predecessor.
func Dijkstra(g *Graph, src int) (dist []int64, prev []int, err error) {
	for u := range g.adj {
		for _, e := range g.adj[u] {
			if e.Weight < 0 {
				return nil, nil, fmt.Errorf("%w: %s -> %s (%d)", ErrNegativeWeight, g.Name(u), g.Name(e.To), e.Weight)
			}
		}
	}
	dist = make([]int64, g.Len())
	prev = make([]int, g.Len())
	for i := range dist {
		dist[i] = Inf
		prev[i] = -1
	}
	dist[src] = 0
	q := &pq{{src, 0}}
	for q.Len() > 0 {
		it := heap.Pop(q).(item)
		if it.dist > dist[it.node] {
			continue
		}
		for _, e := range g.adj[it.node] {
			if nd := it.dist + e.Weight; nd < dist[e.To] {
				dist[e.To] = nd
				prev[e.To] = it.node
				heap.Push(q, item{e.To, nd})
			}
		}
	}
	return dist, prev, nil
}

// PathTo reconstructs the path ending at dst from prev.
func PathTo(prev []int, dst int) []int {
	var path []int
	for v := dst; v != -1; v = prev[v] {
		path = append(path, v)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// BFS returns hop counts from src (-1 for unreachable) in visiting order.
func BFS(g *Graph, src int) (order []int, hops []int) {
	hops = make([]int, g.Len())
	for i := range hops {
		hops[i] = -1
	}
	hops[src] = 0
	queue := []int{src}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		order = append(order, u)
		for _, e := range g.adj[u] {
			if hops[e.To] < 0 {
				hops[e.To] = hops[u] + 1
				queue = append(queue, e.To)
			}
		}
	}
	return order, hops
}
