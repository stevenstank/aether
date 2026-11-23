// Command graph reads a graph from standard input and runs an algorithm on
// it.
//
//	graph stats
//	graph path FROM TO      shortest weighted path (Dijkstra)
//	graph hops FROM         breadth-first hop counts
//	graph topo              topological order
//	graph scc               strongly connected components
//	graph mst               minimum spanning forest (undirected graphs)
//	graph far FROM          the K farthest reachable nodes by distance (K=10)
//
// Exit status: 1 for an algorithm error (unreachable, cycle, ...), 2 for bad
// usage or malformed input.
package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

func die(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, "graph: "+format+"\n", args...)
	os.Exit(code)
}

func names(g *Graph, ids []int) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = g.Name(id)
	}
	return strings.Join(s, " -> ")
}

func main() {
	if len(os.Args) < 2 {
		die(2, "usage: graph stats|path|hops|topo|scc|mst|far [args]")
	}
	cmd, args := os.Args[1], os.Args[2:]
	g, err := Parse(os.Stdin)
	if err != nil {
		var pe *ParseError
		if errors.As(err, &pe) {
			die(2, "malformed input: %v", pe)
		}
		die(2, "%v", err)
	}
	node := func(i int) int {
		if i >= len(args) {
			die(2, "%s: missing node argument", cmd)
		}
		id, err := g.Lookup(args[i])
		if err != nil {
			die(1, "%v", err)
		}
		return id
	}

	switch cmd {
	case "stats":
		kind := "directed"
		if !g.Directed {
			kind = "undirected"
		}
		maxDeg, maxNode := -1, ""
		var wsum int64
		for u := 0; u < g.Len(); u++ {
			if d := len(g.adj[u]); d > maxDeg || d == maxDeg && g.Name(u) < maxNode {
				maxDeg, maxNode = d, g.Name(u)
			}
			for _, e := range g.adj[u] {
				wsum += e.Weight
			}
		}
		fmt.Printf("%s graph: %d nodes, %d edges, total weight %d\n", kind, g.Len(), g.Edges(), wsum)
		if g.Len() > 0 {
			fmt.Printf("max out-degree %d at %s\n", maxDeg, maxNode)
		}
	case "path":
		src, dst := node(0), node(1)
		dist, prev, err := Dijkstra(g, src)
		if err != nil {
			die(1, "%v", err)
		}
		if dist[dst] == Inf {
			die(1, "no path from %s to %s", g.Name(src), g.Name(dst))
		}
		p := PathTo(prev, dst)
		fmt.Printf("distance %d, %d hops\n%s\n", dist[dst], len(p)-1, names(g, p))
	case "far":
		src := node(0)
		dist, _, err := Dijkstra(g, src)
		if err != nil {
			die(1, "%v", err)
		}
		var reach []int
		for v, d := range dist {
			if d != Inf {
				reach = append(reach, v)
			}
		}
		sort.Slice(reach, func(i, j int) bool {
			a, b := reach[i], reach[j]
			if dist[a] != dist[b] {
				return dist[a] > dist[b]
			}
			return g.Name(a) < g.Name(b)
		})
		fmt.Printf("%d of %d nodes reachable from %s\n", len(reach), g.Len(), g.Name(src))
		for i := 0; i < 10 && i < len(reach); i++ {
			fmt.Printf("  %-10s %d\n", g.Name(reach[i]), dist[reach[i]])
		}
	case "hops":
		src := node(0)
		order, hops := BFS(g, src)
		levels := map[int][]int{}
		maxH := 0
		for _, v := range order {
			levels[hops[v]] = append(levels[hops[v]], v)
			maxH = max(maxH, hops[v])
		}
		for h := 0; h <= maxH; h++ {
			fmt.Printf("%d: %s\n", h, strings.Join(g.SortedNames(levels[h]), " "))
		}
		if un := g.Len() - len(order); un > 0 {
			fmt.Printf("unreachable: %d\n", un)
		}
	case "topo":
		order, err := TopoSort(g)
		if err != nil {
			var ce *CycleError
			if errors.As(err, &ce) {
				fmt.Println("cycle length:", len(ce.Cycle)-1)
			}
			die(1, "%v", err)
		}
		fmt.Println(strings.ReplaceAll(names(g, order), " -> ", " "))
	case "scc":
		comps := SCC(g)
		big := 0
		for _, c := range comps {
			big = max(big, len(c))
		}
		fmt.Printf("%d components, largest %d\n", len(comps), big)
		shown := 0
		for _, c := range comps {
			if len(c) > 1 && shown < 20 {
				fmt.Printf("  {%s}\n", strings.Join(c, " "))
				shown++
			}
		}
	case "mst":
		edges, total, trees, err := MST(g)
		if err != nil {
			die(1, "%v", err)
		}
		fmt.Printf("weight %d, %d edges, %d tree(s)\n", total, len(edges), trees)
		for i, e := range edges {
			if i == 15 {
				fmt.Printf("  ... %d more\n", len(edges)-15)
				break
			}
			fmt.Printf("  %s - %s %d\n", g.Name(e.u), g.Name(e.v), e.w)
		}
	default:
		die(2, "unknown command %q", cmd)
	}
}
