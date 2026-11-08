package ir

import (
	"errors"
	"slices"
)

// Promote converts f from ordinary form to SSA form (D-025, the D-006
// experiment): every slot becomes SSA values, every load is replaced by the
// value stored last on the path to it, and phis merge the values where
// paths join. f must be well formed, as Verify checks; then the result is
// too.
//
// Phis are pruned: a slot gets a phi only at a block in the iterated
// dominance frontier of its stores (Cytron et al., 1991) where the slot is
// live on entry (placePhis). The ordinary form's definite-assignment rule
// then gives every phi a value on every incoming edge, so no undefined value
// is needed.
// New phis are numbered after f's existing values, by block, then by slot.
// Promotion removes instructions but never moves or reorders the others,
// so calls, panics, and global accesses keep their order (D-024).
//
// Promote returns an error, leaving f unchanged, if f is already in SSA
// form, if an edge enters its entry block (which SSA form forbids), or if a
// load has no reaching store, which Verify excludes.
func Promote(f *Func) error {
	if f.SSA {
		return errors.New("promote: func " + f.Name + " is already in SSA form")
	}
	d := Dominators(f)
	if len(d.Preds(f.Entry())) > 0 {
		return errors.New("promote: func " + f.Name + ": an edge enters the entry block")
	}
	n, ns := len(f.Blocks), len(f.Slots)
	phis, slotOf := placePhis(f, d)

	// Rename over the dominator tree, keeping for each slot a stack of the
	// values that reach the current block. Changes are recorded and applied
	// only at the end, so that an error leaves f unchanged.
	type rewrite struct {
		in  *Instr
		arg int
		v   *Instr
	}
	var rewrites []rewrite
	repl := map[*Instr]*Instr{}              // each load's value
	incoming := map[*Instr]map[Edge]*Instr{} // each phi's value along each edge
	current := make([][]*Instr, ns)          // the stacks
	top := func(s int) *Instr {
		if st := current[s]; len(st) > 0 {
			return st[len(st)-1]
		}
		return nil
	}
	type frame struct {
		b      *Block
		pushed []int // the slots pushed in b, popped on leaving it
		next   int   // the next dominator-tree child to visit
	}
	enter := func(b *Block) (frame, error) {
		fr := frame{b: b}
		push := func(s int, v *Instr) {
			current[s] = append(current[s], v)
			fr.pushed = append(fr.pushed, s)
		}
		for _, phi := range phis[b.ID] {
			push(slotOf[phi], phi)
		}
		for _, in := range b.Instrs {
			for j, a := range in.Args {
				if v, ok := repl[a]; ok {
					rewrites = append(rewrites, rewrite{in, j, v})
				}
			}
			switch in.Op {
			case Load:
				v := top(in.Slot.ID)
				if v == nil {
					return fr, errors.New("promote: func " + f.Name + ": load of " + in.Slot.Name + " with no reaching store")
				}
				repl[in] = v
			case Store:
				v := in.Args[0]
				if r, ok := repl[v]; ok {
					v = r
				}
				push(in.Slot.ID, v)
			}
		}
		for k, t := range Succs(b) {
			for _, phi := range phis[t.ID] {
				v := top(slotOf[phi])
				if v == nil {
					return fr, errors.New("promote: func " + f.Name + ": no value of " + f.Slots[slotOf[phi]].Name + " along an edge into a phi")
				}
				if incoming[phi] == nil {
					incoming[phi] = map[Edge]*Instr{}
				}
				incoming[phi][Edge{b, k}] = v
			}
		}
		return fr, nil
	}
	fr, err := enter(f.Entry())
	if err != nil {
		return err
	}
	for walk := []frame{fr}; len(walk) > 0; {
		fr := &walk[len(walk)-1]
		kids := d.Children(fr.b)
		if fr.next == len(kids) {
			for _, s := range fr.pushed {
				current[s] = current[s][:len(current[s])-1]
			}
			walk = walk[:len(walk)-1]
			continue
		}
		kid := kids[fr.next]
		fr.next++
		next, err := enter(kid)
		if err != nil {
			return err
		}
		walk = append(walk, next)
	}

	// Apply the changes: number the phis and give them their values in
	// canonical edge order, rewrite uses of loads, and drop the loads, the
	// stores, and the slots.
	edges := make([][]Edge, n) // each block's incoming edges, in canonical order
	for _, b := range f.Blocks {
		if d.Reachable(b) {
			for k, t := range Succs(b) {
				edges[t.ID] = append(edges[t.ID], Edge{b, k})
			}
		}
	}
	for _, b := range f.Blocks {
		for _, phi := range phis[b.ID] {
			phi.ID = f.nextValue
			f.nextValue++
			for _, e := range edges[b.ID] {
				phi.AddIncoming(e, incoming[phi][e])
			}
		}
	}
	for _, r := range rewrites {
		r.in.Args[r.arg] = r.v
	}
	for _, b := range f.Blocks {
		instrs := append([]*Instr(nil), phis[b.ID]...)
		for _, in := range b.Instrs {
			if in.Op != Load && in.Op != Store {
				instrs = append(instrs, in)
			}
		}
		b.Instrs = instrs
	}
	f.Slots, f.SSA = nil, true
	return nil
}

// placePhis places the phis of f's slots, by block and in slot order within
// a block, and records each phi's slot. A slot gets a phi at a block y in the
// iterated dominance frontier of its stores where it is live on entry; a phi
// is a store of its own, so the frontier of y is visited in turn.
//
// The work is sparse, slot by slot, with arrays shared by all slots and
// stamped with the slot (no clearing): for each slot, the blocks that store
// it and the blocks that load it before any store in the block are listed in
// one pass over the instructions. Liveness is asked only at the blocks the
// iterated frontier reaches. A block that loads the slot before storing it
// is live on entry, which answers for a loop header that tests its counter;
// a block outside the dominator subtrees in which the slot's loads can see
// an entry value (loadScopes) is not, which answers for an outer loop's
// header and an inner loop's counter. Otherwise liveness is computed once for
// the slot, by a backward search from its loads that stops at the blocks
// storing it, so each block enters it at most once. A slot then costs its
// stores, its loads, the blocks its pruned iterated frontier reaches, and,
// only where neither answer applies, the blocks where it is live.
func placePhis(f *Func, d *DomTree) ([][]*Instr, map[*Instr]int) {
	n, ns := len(f.Blocks), len(f.Slots)
	storesIn, upwardIn := make([][]int, ns), make([][]int, ns)     // block IDs, by slot
	blockStores, blockUpward := make([][]int, n), make([][]int, n) // slots, by block
	seenStore, seenLoad := make([]int, ns), make([]int, ns)        // stamp: block ID + 1
	for _, b := range f.Blocks {
		stamp := b.ID + 1
		for _, in := range b.Instrs {
			switch in.Op {
			case Load:
				s := in.Slot.ID
				if seenStore[s] != stamp && seenLoad[s] != stamp {
					seenLoad[s] = stamp
					upwardIn[s] = append(upwardIn[s], b.ID)
					blockUpward[b.ID] = append(blockUpward[b.ID], s)
				}
			case Store:
				s := in.Slot.ID
				if seenStore[s] != stamp {
					seenStore[s] = stamp
					storesIn[s] = append(storesIn[s], b.ID)
					blockStores[b.ID] = append(blockStores[b.ID], s)
				}
			}
		}
	}
	var scopes slotScopes // computed when first needed
	phis := make([][]*Instr, n)
	slotOf := map[*Instr]int{}
	// Per-block marks, stamped with the slot + 1.
	stores, upward, live, has, queued := make([]int, n), make([]int, n), make([]int, n), make([]int, n), make([]int, n)
	var work []int
	for s, slot := range f.Slots {
		stamp := s + 1
		frontier := false
		for _, b := range storesIn[s] {
			stores[b] = stamp
			frontier = frontier || len(d.df[b]) > 0
		}
		if !frontier {
			continue // no block can need a phi of s
		}
		for _, b := range upwardIn[s] {
			upward[b] = stamp
		}
		// liveIn reports whether s is live on entry to y: the least fixed
		// point of live(b) = upward(b) ∪ (∪ live(succ) − stores(b)). A block
		// that loads s before storing it is live, and one outside every
		// scope of s's loads (loadScopes) is not; otherwise the first such
		// question computes the fixed point, as a search backward from the
		// loads, once for s.
		searched := false
		liveIn := func(y int) bool {
			if upward[y] == stamp {
				return true
			}
			if scopes == nil {
				scopes = loadScopes(d, ns, blockStores, blockUpward)
			}
			if !scopes.contain(s, d.pre[y]) {
				return false
			}
			if !searched {
				searched = true
				back := slices.Clone(upwardIn[s])
				for _, b := range back {
					live[b] = stamp
				}
				for len(back) > 0 {
					b := back[len(back)-1]
					back = back[:len(back)-1]
					for _, p := range d.preds[b] {
						if live[p] != stamp && stores[p] != stamp {
							live[p] = stamp
							back = append(back, p)
						}
					}
				}
			}
			return live[y] == stamp
		}
		work = work[:0]
		for _, b := range storesIn[s] {
			queued[b] = stamp
			work = append(work, b)
		}
		for len(work) > 0 {
			x := work[len(work)-1]
			work = work[:len(work)-1]
			for _, y := range d.df[x] {
				if has[y] == stamp || !liveIn(y) {
					continue
				}
				has[y] = stamp
				phi := &Instr{Op: Phi, Type: slot.Type, Pos: slot.Pos}
				phis[y] = append(phis[y], phi)
				slotOf[phi] = s
				if queued[y] != stamp {
					queued[y] = stamp
					work = append(work, y)
				}
			}
		}
	}
	return phis, slotOf
}

// slotScopes are, for each slot, disjoint intervals of dominator-tree
// preorder numbers, sorted, outside which no load of the slot can make it
// live on entry.
type slotScopes [][][2]int

func (sc slotScopes) contain(s, pre int) bool {
	iv := sc[s]
	i, _ := slices.BinarySearchFunc(iv, pre, func(x [2]int, p int) int { return x[0] - p })
	if i < len(iv) && iv[i][0] == pre {
		return true
	}
	return i > 0 && pre <= iv[i-1][1]
}

// loadScopes finds, for each load of a slot before any store to it in its
// block, the nearest strict dominator of the block that stores the slot, in
// one walk down the dominator tree with each slot's nearest storing
// ancestor. A path into that dominator's subtree from outside passes through
// it, and so through a store, so the load makes the slot live only at blocks
// in the subtree; without such a dominator, anywhere. The subtrees are
// intervals of preorder numbers (DomTree's pre and post count both enters
// and leaves, so a subtree is [pre, post]), which nest or are disjoint, and
// are merged per slot. Loads in unreachable blocks make no scope.
func loadScopes(d *DomTree, ns int, blockStores, blockUpward [][]int) slotScopes {
	scopes := make(slotScopes, ns)
	nearest := make([]int, ns) // nearest storing ancestor, -1 if none
	for i := range nearest {
		nearest[i] = -1
	}
	type frame struct {
		b, next int
		saved   []int // nearest of the slots b stores, before b
	}
	enter := func(b int) frame {
		for _, s := range blockUpward[b] {
			r := nearest[s]
			if r < 0 {
				r = 0
			}
			scopes[s] = append(scopes[s], [2]int{d.pre[r], d.post[r]})
		}
		fr := frame{b: b}
		for _, s := range blockStores[b] {
			fr.saved = append(fr.saved, nearest[s])
			nearest[s] = b
		}
		return fr
	}
	stack := []frame{enter(0)}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next == len(d.kids[top.b]) {
			for i, s := range blockStores[top.b] {
				nearest[s] = top.saved[i]
			}
			stack = stack[:len(stack)-1]
			continue
		}
		k := d.kids[top.b][top.next]
		top.next++
		stack = append(stack, enter(k))
	}
	for s, iv := range scopes {
		slices.SortFunc(iv, func(x, y [2]int) int {
			if x[0] != y[0] {
				return x[0] - y[0]
			}
			return y[1] - x[1]
		})
		merged := iv[:0]
		for _, x := range iv {
			if k := len(merged); k > 0 && x[0] <= merged[k-1][1] {
				merged[k-1][1] = max(merged[k-1][1], x[1])
				continue
			}
			merged = append(merged, x)
		}
		scopes[s] = merged
	}
	return scopes
}
