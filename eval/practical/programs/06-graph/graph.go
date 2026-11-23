package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Edge is a weighted edge to node To.
type Edge struct {
	To     int
	Weight int64
}

// Graph is a weighted graph with named nodes stored as adjacency lists.
type Graph struct {
	Directed bool
	names    []string
	index    map[string]int
	adj      [][]Edge
	edges    int
}

func NewGraph(directed bool) *Graph {
	return &Graph{Directed: directed, index: map[string]int{}}
}

// Node returns the id of name, adding it if needed.
func (g *Graph) Node(name string) int {
	if id, ok := g.index[name]; ok {
		return id
	}
	id := len(g.names)
	g.names = append(g.names, name)
	g.index[name] = id
	g.adj = append(g.adj, nil)
	return id
}

func (g *Graph) Lookup(name string) (int, error) {
	id, ok := g.index[name]
	if !ok {
		return 0, fmt.Errorf("unknown node %q", name)
	}
	return id, nil
}

func (g *Graph) Name(id int) string { return g.names[id] }
func (g *Graph) Len() int           { return len(g.names) }
func (g *Graph) Edges() int         { return g.edges }

func (g *Graph) AddEdge(from, to int, w int64) {
	g.adj[from] = append(g.adj[from], Edge{to, w})
	if !g.Directed && from != to {
		g.adj[to] = append(g.adj[to], Edge{from, w})
	}
	g.edges++
}

// ParseError describes a malformed line of graph input.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// Parse reads a graph. The first non-comment line may be "directed" or
// "undirected" (default directed). Every other line is "FROM TO [WEIGHT]"
// or a single node name.
func Parse(r io.Reader) (*Graph, error) {
	sc := bufio.NewScanner(r)
	var g *Graph
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(text, '#'); i >= 0 {
			text = strings.TrimSpace(text[:i])
		}
		if text == "" {
			continue
		}
		if g == nil {
			switch text {
			case "directed", "undirected":
				g = NewGraph(text == "directed")
				continue
			}
			g = NewGraph(true)
		}
		f := strings.Fields(text)
		switch len(f) {
		case 1:
			g.Node(f[0])
		case 2, 3:
			w := int64(1)
			if len(f) == 3 {
				v, err := strconv.ParseInt(f[2], 10, 64)
				if err != nil {
					return nil, &ParseError{line, fmt.Sprintf("bad weight %q", f[2])}
				}
				w = v
			}
			g.AddEdge(g.Node(f[0]), g.Node(f[1]), w)
		default:
			return nil, &ParseError{line, fmt.Sprintf("expected FROM TO [WEIGHT], got %d fields", len(f))}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if g == nil {
		g = NewGraph(true)
	}
	return g, nil
}

// SortedNames returns the names of ids in lexical order.
func (g *Graph) SortedNames(ids []int) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = g.names[id]
	}
	sort.Strings(out)
	return out
}
