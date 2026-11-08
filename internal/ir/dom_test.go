package ir

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// cfg builds a function main whose block i continues at succ[i]: ret if
// succ[i] is empty, a jump if it has one entry, and a branch on a constant
// if it has two. Only the shape of the graph matters to the analyses.
func cfg(succ [][]int) *Func {
	p := &Program{}
	f := p.NewFunc("main", "p.go", Pos{1, 1})
	for range len(succ) - 1 {
		f.NewBlock()
	}
	for i, s := range succ {
		b := f.Blocks[i]
		pos := Pos{i + 2, 1}
		switch len(s) {
		case 0:
			f.Ret(b, pos)
		case 1:
			f.Jump(b, f.Blocks[s[0]], pos)
		case 2:
			f.Branch(b, f.BoolConst(b, true, pos), f.Blocks[s[0]], f.Blocks[s[1]], pos)
		default:
			panic("block with more than two successors")
		}
	}
	return f
}

// ids returns the IDs of bs, or -1 for a nil block.
func ids(bs ...*Block) []int {
	out := make([]int, len(bs))
	for i, b := range bs {
		out[i] = -1
		if b != nil {
			out[i] = b.ID
		}
	}
	return out
}

// naiveDominates computes dominance from its definition: a dominates b if
// b is reachable and no path from the entry reaches b without passing
// through a. It is quadratic but independent of DomTree.
func naiveDominates(f *Func) [][]bool {
	n := len(f.Blocks)
	reach := func(avoid int) []bool {
		seen := make([]bool, n)
		if avoid == 0 {
			return seen
		}
		seen[0] = true
		work := []int{0}
		for len(work) > 0 {
			b := work[len(work)-1]
			work = work[:len(work)-1]
			for _, s := range Succs(f.Blocks[b]) {
				if s.ID != avoid && !seen[s.ID] {
					seen[s.ID] = true
					work = append(work, s.ID)
				}
			}
		}
		return seen
	}
	all := reach(-1)
	dom := make([][]bool, n)
	for a := range n {
		without := reach(a)
		dom[a] = make([]bool, n)
		for b := range n {
			dom[a][b] = all[a] && all[b] && !without[b]
		}
	}
	return dom
}

// checkDomTree compares d with the definitions of dominance, immediate
// dominance, and the dominance frontier, computed naively for f, and checks
// that each block's immediate dominator dominates its predecessors.
func checkDomTree(f *Func, d *DomTree) error {
	dom := naiveDominates(f)
	var errs []string
	fail := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	for _, a := range f.Blocks {
		for _, b := range f.Blocks {
			if got := d.Dominates(a, b); got != dom[a.ID][b.ID] {
				fail("Dominates(b%d, b%d) = %v, want %v", a.ID, b.ID, got, dom[a.ID][b.ID])
			}
		}
	}
	for _, b := range f.Blocks {
		if d.Reachable(b) != dom[0][b.ID] {
			fail("Reachable(b%d) = %v", b.ID, d.Reachable(b))
		}
		// The immediate dominator is the strict dominator that every other
		// strict dominator dominates.
		want := -1
		for a := range f.Blocks {
			if a == b.ID || !dom[a][b.ID] {
				continue
			}
			closest := true
			for c := range f.Blocks {
				if c != b.ID && dom[c][b.ID] && !dom[c][a] {
					closest = false
				}
			}
			if closest {
				want = a
			}
		}
		idom := d.Idom(b)
		if got := ids(idom)[0]; got != want {
			fail("Idom(b%d) = b%d, want b%d", b.ID, got, want)
		}
		var preds []int
		for _, p := range f.Blocks {
			for _, s := range Succs(p) {
				if s == b && dom[0][p.ID] {
					preds = append(preds, p.ID)
				}
			}
		}
		if got := ids(d.Preds(b)...); !slices.Equal(got, preds) {
			fail("Preds(b%d) = %v, want %v", b.ID, got, preds)
		}
		for _, p := range d.Preds(b) {
			if idom != nil && !d.Dominates(idom, p) {
				fail("Idom(b%d) = b%d does not dominate predecessor b%d", b.ID, idom.ID, p.ID)
			}
		}
		var df []int
		for _, c := range f.Blocks {
			if dom[b.ID][c.ID] && b != c {
				continue
			}
			for _, p := range d.Preds(c) {
				if dom[b.ID][p.ID] {
					df = append(df, c.ID)
					break
				}
			}
		}
		if got := ids(d.Frontier(b)...); !slices.Equal(got, df) {
			fail("Frontier(b%d) = %v, want %v", b.ID, got, df)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return nil
}

// TestDominators checks the dominator tree and frontiers of hand-built CFGs
// against reviewed tables, and against the naive definitions.
func TestDominators(t *testing.T) {
	tests := []struct {
		name string
		succ [][]int
		idom []int   // by block; -1 for none
		df   [][]int // by block
		// unreachable is the number of blocks Verify must report as
		// unreachable; 0 means f must verify.
		unreachable int
	}{
		{
			name: "single block",
			succ: [][]int{{}},
			idom: []int{-1},
			df:   [][]int{nil},
		},
		{
			name: "straight line",
			succ: [][]int{{1}, {2}, {}},
			idom: []int{-1, 0, 1},
			df:   [][]int{nil, nil, nil},
		},
		{
			// if c { b1 } else { b2 }; b3
			name: "diamond",
			succ: [][]int{{1, 2}, {3}, {3}, {}},
			idom: []int{-1, 0, 0, 0},
			df:   [][]int{nil, {3}, {3}, nil},
		},
		{
			// if c { b1 }; b2
			name: "if without else",
			succ: [][]int{{1, 2}, {2}, {}},
			idom: []int{-1, 0, 0},
			df:   [][]int{nil, {2}, nil},
		},
		{
			// for cond { body }: b1 header, b2 body, b3 exit
			name: "loop",
			succ: [][]int{{1}, {2, 3}, {1}, {}},
			idom: []int{-1, 0, 1, 1},
			df:   [][]int{nil, {1}, {1}, nil},
		},
		{
			// for { if c { continue }; post }: b1 header, b2 body with
			// branch, b3 continue, b4 post, b5 exit
			name: "loop with continue",
			succ: [][]int{{1}, {2, 5}, {3, 4}, {1}, {1}, {}},
			idom: []int{-1, 0, 1, 2, 2, 1},
			df:   [][]int{nil, {1}, {1}, {1}, {1}, nil},
		},
		{
			// b1 outer header, b2 inner header, b3 inner body, b4 outer
			// latch, b5 exit
			name: "nested loops",
			succ: [][]int{{1}, {2, 5}, {3, 4}, {2}, {1}, {}},
			idom: []int{-1, 0, 1, 2, 2, 1},
			df:   [][]int{nil, {1}, {1, 2}, {2}, {1}, nil},
		},
		{
			// for { for { if c { break } } }: the inner loop's break goes
			// to the outer latch.
			name: "nested loops with break",
			succ: [][]int{{1}, {2, 6}, {3, 5}, {4, 5}, {2}, {1}, {}},
			idom: []int{-1, 0, 1, 2, 3, 2, 1},
			df:   [][]int{nil, {1}, {1, 2}, {2, 5}, {2}, {1}, nil},
		},
		{
			// switch x { case 1: b2; case 2: b4; default: b5 }; b6 join.
			// The case tests chain: b0 tests case 1, b1 tests case 2.
			name: "switch chain",
			succ: [][]int{{2, 1}, {4, 3}, {6}, {5}, {6}, {6}, {}},
			idom: []int{-1, 0, 0, 1, 1, 3, 0},
			df:   [][]int{nil, {6}, {6}, {6}, {6}, {6}, nil},
		},
		{
			// A branch whose two targets are the same block.
			name: "duplicate edge",
			succ: [][]int{{1, 1}, {}},
			idom: []int{-1, 0},
			df:   [][]int{nil, nil},
		},
		{
			// A loop back to the entry: the entry is in its own frontier.
			name: "entry loop",
			succ: [][]int{{1, 2}, {0}, {}},
			idom: []int{-1, 0, 0},
			df:   [][]int{{0}, {0}, nil},
		},
		{
			// b2 and b3 form a cycle no path from the entry reaches; b3
			// also jumps into reachable code.
			name:        "unreachable code",
			succ:        [][]int{{1}, {}, {3}, {2, 1}},
			idom:        []int{-1, 0, -1, -1},
			df:          [][]int{nil, nil, nil, nil},
			unreachable: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cfg(tt.succ)
			errs := Verify(&Program{Funcs: []*Func{f}})
			n := 0
			for _, e := range errs {
				if !strings.Contains(e.Error(), "unreachable block") {
					t.Fatalf("Verify: %v", e)
				}
				n++
			}
			if n != tt.unreachable {
				t.Fatalf("Verify reported %d unreachable blocks, want %d: %v", n, tt.unreachable, errs)
			}
			d := Dominators(f)
			for _, b := range f.Blocks {
				if got := ids(d.Idom(b))[0]; got != tt.idom[b.ID] {
					t.Errorf("Idom(b%d) = %d, want %d", b.ID, got, tt.idom[b.ID])
				}
				if got := ids(d.Frontier(b)...); !slices.Equal(got, tt.df[b.ID]) {
					t.Errorf("Frontier(b%d) = %v, want %v", b.ID, got, tt.df[b.ID])
				}
				for _, k := range d.Children(b) {
					if d.Idom(k) != b {
						t.Errorf("child b%d of b%d has Idom %v", k.ID, b.ID, ids(d.Idom(k)))
					}
				}
			}
			if err := checkDomTree(f, d); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestDominatorsLongChain checks that a CFG deeper than any recursion
// would comfortably allow is handled: the walks are iterative.
func TestDominatorsLongChain(t *testing.T) {
	const n = 200000
	succ := make([][]int, n)
	for i := range n - 1 {
		succ[i] = []int{i + 1}
	}
	f := cfg(succ)
	d := Dominators(f)
	last := f.Blocks[n-1]
	if !d.Dominates(f.Entry(), last) || d.Idom(last) != f.Blocks[n-2] || d.Dominates(last, f.Entry()) {
		t.Error("wrong dominance on a long chain")
	}
}

// TestDominatorsGenerated cross-checks DomTree against the naive
// definitions on every CFG of up to four blocks with at most two
// successors per block, including unreachable blocks and duplicate edges.
func TestDominatorsGenerated(t *testing.T) {
	var shapes [][]int // the possible successor lists for n blocks
	count := 0
	for n := 1; n <= 4; n++ {
		shapes = [][]int{{}}
		for a := range n {
			shapes = append(shapes, []int{a})
			for b := range n {
				shapes = append(shapes, []int{a, b})
			}
		}
		succ := make([][]int, n)
		var walk func(i int)
		walk = func(i int) {
			if i == n {
				f := cfg(succ)
				if err := checkDomTree(f, Dominators(f)); err != nil {
					t.Fatalf("CFG %v:\n%v", succ, err)
				}
				count++
				return
			}
			for _, s := range shapes {
				succ[i] = s
				walk(i + 1)
			}
		}
		walk(0)
	}
	t.Logf("%d CFGs checked", count)
}

// TestDominatorsRandom cross-checks DomTree against the naive definitions on
// random CFGs of up to 60 blocks, where path compression and long frontier
// walks matter, with unreachable blocks and duplicate edges.
func TestDominatorsRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 400 {
		n := 2 + r.IntN(59)
		succ := make([][]int, n)
		for i := range succ {
			switch r.IntN(4) {
			case 0: // ret
			case 1:
				succ[i] = []int{r.IntN(n)}
			default:
				succ[i] = []int{r.IntN(n), r.IntN(n)}
			}
		}
		f := cfg(succ)
		if err := checkDomTree(f, Dominators(f)); err != nil {
			t.Fatalf("CFG %v:\n%v", succ, err)
		}
	}
}
