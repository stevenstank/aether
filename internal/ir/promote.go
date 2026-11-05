package ir

import "errors"

// Promote converts f from ordinary form to SSA form (D-025, the D-006
// experiment): every slot becomes SSA values, every load is replaced by the
// value stored last on the path to it, and phis merge the values where
// paths join. f must be well formed, as Verify checks; then the result is
// too.
//
// Phis are pruned: a slot gets a phi only at a block in the iterated
// dominance frontier of its stores (Cytron et al., 1991) where the slot is
// live on entry. The ordinary form's definite-assignment rule then gives
// every phi a value on every incoming edge, so no undefined value is needed.
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

	// upward[b][s]: b loads s before any store to s in b; stores[b][s]: b
	// stores s.
	upward, stores := make([][]bool, n), make([][]bool, n)
	for _, b := range f.Blocks {
		upward[b.ID], stores[b.ID] = make([]bool, ns), make([]bool, ns)
		for _, in := range b.Instrs {
			switch in.Op {
			case Load:
				upward[b.ID][in.Slot.ID] = upward[b.ID][in.Slot.ID] || !stores[b.ID][in.Slot.ID]
			case Store:
				stores[b.ID][in.Slot.ID] = true
			}
		}
	}
	// live[b][s]: s is live on entry to b, the least fixed point of
	// live(b) = upward(b) ∪ (∪ live(succ) − stores(b)).
	live := make([][]bool, n)
	for i := range live {
		live[i] = append([]bool(nil), upward[i]...)
	}
	for changed := true; changed; {
		changed = false
		for i := n - 1; i >= 0; i-- {
			for _, succ := range Succs(f.Blocks[i]) {
				for s := range ns {
					if live[succ.ID][s] && !stores[i][s] && !live[i][s] {
						live[i][s] = true
						changed = true
					}
				}
			}
		}
	}

	// Place the phis, which are inserted only once renaming succeeds.
	phis := make([][]*Instr, n) // by block, in slot order
	slotOf := map[*Instr]int{}
	for s, slot := range f.Slots {
		has, queued := make([]bool, n), make([]bool, n)
		var work []*Block
		for _, b := range f.Blocks {
			if stores[b.ID][s] {
				queued[b.ID] = true
				work = append(work, b)
			}
		}
		for len(work) > 0 {
			x := work[len(work)-1]
			work = work[:len(work)-1]
			for _, y := range d.Frontier(x) {
				if has[y.ID] || !live[y.ID][s] {
					continue
				}
				has[y.ID] = true
				phi := &Instr{Op: Phi, Type: slot.Type, Pos: slot.Pos}
				phis[y.ID] = append(phis[y.ID], phi)
				slotOf[phi] = s
				if !queued[y.ID] {
					queued[y.ID] = true
					work = append(work, y)
				}
			}
		}
	}

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
