package ir

import "slices"

// DomTree is the dominator tree of a function's control-flow graph, with
// each block's predecessors and dominance frontier: the analyses that slot
// promotion (D-006) builds on. Block a dominates block b if every path from
// the entry to b passes through a; every reachable block dominates itself.
// The immediate dominator of a reachable block other than the entry is its
// closest strict dominator.
//
// Only blocks reachable from the entry take part. Verify rejects
// unreachable blocks, but DomTree tolerates them: such a block has no
// immediate dominator, dominates nothing, is dominated by nothing, and
// appears in no predecessor list or frontier.
//
// Every list is in a deterministic order that depends only on the CFG.
type DomTree struct {
	f     *Func
	idom  []int   // block ID of the immediate dominator; -1 for the entry and unreachable blocks
	preds [][]int // reachable predecessors, one entry per edge
	kids  [][]int // children in the dominator tree, by block ID
	df    [][]int // dominance frontier, by block ID
	// pre and post number the dominator tree in a depth-first walk, so that
	// a dominates b exactly if a's interval contains b's.
	pre, post []int
}

// Succs returns the blocks b's terminator may continue at, in target
// order: none for ret and panic, one for jump, and two for branch, which
// may name the same block twice.
func Succs(b *Block) []*Block {
	if len(b.Instrs) == 0 {
		return nil
	}
	return b.Instrs[len(b.Instrs)-1].Targets
}

// Dominators computes f's dominator tree and dominance frontiers. f must
// have at least one block, and every jump and branch target must be a block
// of f, as Verify checks.
//
// It finds immediate dominators with the algorithm of Lengauer and Tarjan,
// "A Fast Algorithm for Finding Dominators in a Flowgraph" (1979), in its
// simple form (path compression without balancing, O(m log n)), and
// frontiers with the algorithm of Cooper, Harvey, and Kennedy, "A Simple,
// Fast Dominance Algorithm" (2001), whose walks stop where an earlier walk
// for the same block went on, so they cost the frontiers' size and the edges.
func Dominators(f *Func) *DomTree {
	n := len(f.Blocks)
	d := &DomTree{
		f:     f,
		idom:  make([]int, n),
		preds: make([][]int, n),
		kids:  make([][]int, n),
		df:    make([][]int, n),
		pre:   make([]int, n),
		post:  make([]int, n),
	}
	reachable := make([]bool, n)
	for i := range d.idom {
		d.idom[i] = -1
	}
	for _, b := range postorder(f) {
		reachable[b] = true
	}
	for _, b := range f.Blocks {
		if !reachable[b.ID] {
			continue
		}
		for _, s := range Succs(b) {
			d.preds[s.ID] = append(d.preds[s.ID], b.ID)
		}
	}
	for i := range d.preds {
		slices.Sort(d.preds[i])
	}

	d.lengauerTarjan()

	for b, id := range d.idom {
		if id >= 0 {
			d.kids[id] = append(d.kids[id], b) // in increasing block ID
		}
	}
	d.number()

	// b is added to frontiers only while b is visited here, so a frontier
	// that already holds b ends with it.
	for b, ps := range d.preds {
		for _, p := range ps {
			for r := p; r != d.idom[b]; r = d.idom[r] {
				if k := len(d.df[r]); k > 0 && d.df[r][k-1] == b {
					break // an earlier walk for b went on from r to b's idom
				}
				d.df[r] = append(d.df[r], b)
				if r == 0 {
					break // the entry: b is a loop back to it
				}
			}
		}
	}
	for i := range d.df {
		slices.Sort(d.df[i])
	}
	return d
}

// lengauerTarjan sets idom of the reachable blocks other than the entry,
// given preds.
func (d *DomTree) lengauerTarjan() {
	n := len(d.f.Blocks)
	// Number the reachable blocks in a depth-first preorder.
	dfnum, parent := make([]int, n), make([]int, n)
	for i := range dfnum {
		dfnum[i], parent[i] = -1, -1
	}
	var vertex []int
	type frame struct{ b, next int }
	stack := []frame{{b: 0}}
	dfnum[0] = 0
	vertex = append(vertex, 0)
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		succ := Succs(d.f.Blocks[top.b])
		if top.next == len(succ) {
			stack = stack[:len(stack)-1]
			continue
		}
		s := succ[top.next].ID
		top.next++
		if dfnum[s] < 0 {
			dfnum[s], parent[s] = len(vertex), top.b
			vertex = append(vertex, s)
			stack = append(stack, frame{b: s})
		}
	}
	semi, ancestor, label := make([]int, n), make([]int, n), make([]int, n)
	for i := range semi {
		semi[i], ancestor[i], label[i] = dfnum[i], -1, i
	}
	// eval returns the vertex of least semidominator on the forest path
	// to v, compressing the path iteratively.
	var path []int
	eval := func(v int) int {
		if ancestor[v] < 0 {
			return v
		}
		path = path[:0]
		for x := v; ancestor[ancestor[x]] >= 0; x = ancestor[x] {
			path = append(path, x)
		}
		for i := len(path) - 1; i >= 0; i-- {
			y := path[i]
			a := ancestor[y]
			if semi[label[a]] < semi[label[y]] {
				label[y] = label[a]
			}
			ancestor[y] = ancestor[a]
		}
		return label[v]
	}
	bucket := make([][]int, n)
	for i := len(vertex) - 1; i > 0; i-- {
		w := vertex[i]
		for _, v := range d.preds[w] {
			if u := eval(v); semi[u] < semi[w] {
				semi[w] = semi[u]
			}
		}
		sv := vertex[semi[w]]
		bucket[sv] = append(bucket[sv], w)
		p := parent[w]
		ancestor[w] = p
		for _, v := range bucket[p] {
			if u := eval(v); semi[u] < semi[v] {
				d.idom[v] = u
			} else {
				d.idom[v] = p
			}
		}
		bucket[p] = nil
	}
	for _, w := range vertex[1:] {
		if d.idom[w] != vertex[semi[w]] {
			d.idom[w] = d.idom[d.idom[w]]
		}
	}
}

// postorder returns the block IDs reachable from f's entry in the
// postorder of a depth-first search that visits successors in target
// order.
func postorder(f *Func) []int {
	seen := make([]bool, len(f.Blocks))
	type frame struct {
		b    *Block
		next int // index of the next successor to visit
	}
	var order []int
	stack := []frame{{b: f.Blocks[0]}}
	seen[0] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		succ := Succs(top.b)
		if top.next == len(succ) {
			order = append(order, top.b.ID)
			stack = stack[:len(stack)-1]
			continue
		}
		s := succ[top.next]
		top.next++
		if !seen[s.ID] {
			seen[s.ID] = true
			stack = append(stack, frame{b: s})
		}
	}
	return order
}

// number assigns pre and post over the dominator tree, iteratively so that
// a long chain of blocks cannot exhaust the Go stack.
func (d *DomTree) number() {
	for i := range d.pre {
		d.pre[i], d.post[i] = -1, -1
	}
	type frame struct{ b, next int }
	clock := 0
	stack := []frame{{b: 0}}
	d.pre[0] = clock
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next == len(d.kids[top.b]) {
			clock++
			d.post[top.b] = clock
			stack = stack[:len(stack)-1]
			continue
		}
		k := d.kids[top.b][top.next]
		top.next++
		clock++
		d.pre[k] = clock
		stack = append(stack, frame{b: k})
	}
}

func (d *DomTree) blocks(ids []int) []*Block {
	bs := make([]*Block, len(ids))
	for i, id := range ids {
		bs[i] = d.f.Blocks[id]
	}
	return bs
}

// Reachable reports whether a path from the entry reaches b.
func (d *DomTree) Reachable(b *Block) bool { return d.pre[b.ID] >= 0 }

// Idom returns b's immediate dominator, or nil if b is the entry or
// unreachable.
func (d *DomTree) Idom(b *Block) *Block {
	if id := d.idom[b.ID]; id >= 0 {
		return d.f.Blocks[id]
	}
	return nil
}

// Dominates reports whether a dominates b. A reachable block dominates
// itself.
func (d *DomTree) Dominates(a, b *Block) bool {
	if !d.Reachable(a) || !d.Reachable(b) {
		return false
	}
	return d.pre[a.ID] <= d.pre[b.ID] && d.post[b.ID] <= d.post[a.ID]
}

// Children returns the blocks b immediately dominates, in block ID order.
func (d *DomTree) Children(b *Block) []*Block { return d.blocks(d.kids[b.ID]) }

// Preds returns b's reachable predecessors in block ID order, one entry
// per edge, so a branch whose two targets are b contributes twice.
func (d *DomTree) Preds(b *Block) []*Block { return d.blocks(d.preds[b.ID]) }

// Frontier returns b's dominance frontier in block ID order: the blocks
// with a predecessor that b dominates but that b does not strictly
// dominate themselves.
func (d *DomTree) Frontier(b *Block) []*Block { return d.blocks(d.df[b.ID]) }
