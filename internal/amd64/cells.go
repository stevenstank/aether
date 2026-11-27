package amd64

// Frame cells and slots shared by values and variables never live at once
// (D-058).
//
// Each value without a register or an immediate needs a cell, and a Tuple
// call one cell per result; each slot of a function not in SSA form needs
// a word. Giving every one its own made a frame grow with a function's
// size, not with what it keeps at once: a large function's frame was
// kilobytes, and a recursion through it overflowed the stack (V-2) at a
// fraction of gc's depth. shareCells colors them instead, as the register
// allocator colors registers, with as many colors as it needs:
//   - In a function not in SSA form, a value is used only in its own block
//     (D-023), so a block's values are colored by a scan of the block, each
//     cell free again after its value's last use; every block starts with
//     all of them free.
//   - In an SSA function within maxAllocValues, the values are colored in
//     dominator-tree preorder from their liveness (regalloc.go): at its
//     definition a value takes the first free cells, those of the values
//     live into the block and still live, or defined since and not dead,
//     being taken. A larger SSA function keeps one cell per value.
//   - A slot of a function not in SSA form is live from a store to the
//     loads it reaches; slots live at once, or one live where the other is
//     stored, interfere, and are colored greedily. Lowering stores every
//     slot before loading it on each path (D-023 invariant 4), so a slot
//     is live into the entry only if its loads are in unreachable code.
//
// Every value's operands keep their cells until after its definition, and
// an operand of a value with no location (a fused comparison) keeps its
// cell to the end of the block, where the Branch reads it. The φs of a
// block take distinct cells. checkCells verifies the result independently,
// and the collector scans the frame conservatively whatever it holds
// (D-046).

import (
	"fmt"
	"math/bits"
	"slices"

	"github.com/stevenstank/aether/internal/ir"
)

// maxSlotColors bounds the slots shareSlots colors: its interference
// matrix has the square of their number in bits, 2 MiB at the bound. A
// function with more keeps one word per slot.
const maxSlotColors = 4096

// cellPlan is where the values without registers live: the first cell of
// each value and of each Tuple call, and the number of cells in all.
type cellPlan struct {
	cells, tuples map[*ir.Instr]int
	n             int
}

// cellWidth is the cells v needs: one per result of a Tuple call, else one.
func cellWidth(v *ir.Instr) int {
	if v.Type == ir.Tuple {
		return len(v.CallResults())
	}
	return 1
}

// cellSet tracks which value holds each cell while a block is colored.
type cellSet struct {
	holder []*ir.Instr
	first  map[*ir.Instr]int
	pinned valueSet // values that hold their cells everywhere
	lo     int      // no cell below lo is free
}

// take gives v the first run of cellWidth(v) free cells, unless it is
// pinned and has them.
func (s *cellSet) take(v *ir.Instr) {
	if s.pinned[v] {
		return
	}
	w := cellWidth(v)
	for s.lo < len(s.holder) && s.holder[s.lo] != nil {
		s.lo++
	}
	for c := s.lo; ; c++ {
		for len(s.holder) < c+w {
			s.holder = append(s.holder, nil)
		}
		if !slices.ContainsFunc(s.holder[c:c+w], func(h *ir.Instr) bool { return h != nil }) {
			for i := c; i < c+w; i++ {
				s.holder[i] = v
			}
			s.first[v] = c
			return
		}
	}
}

// hold marks v's cells, which it was given before, taken.
func (s *cellSet) hold(v *ir.Instr) {
	c, w := s.first[v], cellWidth(v)
	for len(s.holder) < c+w {
		s.holder = append(s.holder, nil)
	}
	for i := c; i < c+w; i++ {
		s.holder[i] = v
	}
}

// release frees v's cells if v holds them and is not pinned.
func (s *cellSet) release(v *ir.Instr) {
	c, ok := s.first[v]
	if !ok || s.pinned[v] {
		return
	}
	for i := c; i < c+cellWidth(v) && i < len(s.holder); i++ {
		if s.holder[i] == v {
			s.holder[i] = nil
			s.lo = min(s.lo, i)
		}
	}
}

// clear frees every cell but the pinned values'.
func (s *cellSet) clear() {
	s.lo = 0
	for i, v := range s.holder {
		if !s.pinned[v] {
			s.holder[i] = nil
		}
	}
}

// shareCells assigns cells to the values of f that need them: those with
// no register (regs) and a location (not in none), each Tuple call's
// results included. Under ABI-R1 (r1), in a function not in SSA form, the
// params with uses have cells of their own: the entry copies those in
// argument registers all at once (abir1.go), and checkR1 wants every
// used param's location distinct.
func shareCells(f *ir.Func, regs map[*ir.Instr]int, none valueSet, r1, unique bool) cellPlan {
	need := func(v *ir.Instr) bool {
		if v.ID < 0 || v.Type == ir.NoType || none[v] {
			return false
		}
		_, ok := regs[v]
		return !ok && cellWidth(v) > 0
	}
	s := &cellSet{first: map[*ir.Instr]int{}, pinned: valueSet{}}
	// In SSA form nothing but params runs before the last one, so a param
	// with a use is live at each later one's definition anyway; before
	// promotion, a store of one may come first, and its cell be taken.
	if r1 && !f.SSA {
		uses := useCounts(f)
		for _, in := range f.Entry().Instrs {
			if in.Op == ir.Param && uses[in] > 0 && need(in) {
				s.take(in)
				s.pinned[in] = true
			}
		}
	}
	switch {
	case unique:
		var vals []*ir.Instr
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				if need(in) {
					vals = append(vals, in)
				}
			}
		}
		// In value-number order, the tuples' cells after the values'.
		slices.SortStableFunc(vals, func(x, y *ir.Instr) int {
			if (x.Type == ir.Tuple) != (y.Type == ir.Tuple) {
				if x.Type == ir.Tuple {
					return 1
				}
				return -1
			}
			return x.ID - y.ID
		})
		for _, v := range vals {
			s.take(v)
		}
	case !f.SSA:
		for _, b := range f.Blocks {
			s.clear()
			last := lastUse(b)
			s.colorBlock(b, need, none, func(v *ir.Instr, j int) bool { return last[v] > j })
		}
	case countValues(f) <= maxAllocValues:
		d := ir.Dominators(f)
		lv := liveness(f, d)
		var buf []uint64
		work := []*ir.Block{f.Entry()}
		for len(work) > 0 {
			b := work[len(work)-1]
			work = work[:len(work)-1]
			s.clear()
			lv.each(lv.liveIn(b), func(v *ir.Instr) {
				if need(v) {
					s.hold(v)
				}
			})
			var after [][]uint64
			after, buf = lv.afterSets(b, buf)
			s.colorBlock(b, need, none, func(v *ir.Instr, j int) bool { return lv.has(after[j], v) })
			kids := d.Children(b)
			for i := len(kids) - 1; i >= 0; i-- {
				work = append(work, kids[i])
			}
		}
		// Unreachable blocks never run: their values may share any cells.
		for _, b := range f.Blocks {
			if !d.Reachable(b) {
				s.clear()
				for _, in := range b.Instrs {
					if need(in) {
						s.take(in)
						s.release(in)
					}
				}
			}
		}
	default:
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				if need(in) {
					s.take(in)
				}
			}
		}
	}
	p := cellPlan{cells: map[*ir.Instr]int{}, tuples: map[*ir.Instr]int{}, n: len(s.holder)}
	for v, c := range s.first {
		if v.Type == ir.Tuple {
			p.tuples[v] = c
		} else {
			p.cells[v] = c
		}
		p.n = max(p.n, c+cellWidth(v))
	}
	return p
}

// colorBlock colors the values of b that need cells, the cells of those
// live into it held already. live reports whether v is live after the
// instruction at j.
func (s *cellSet) colorBlock(b *ir.Block, need func(*ir.Instr) bool, none valueSet, live func(v *ir.Instr, j int) bool) {
	phis := 0
	for phis < len(b.Instrs) && b.Instrs[phis].Op == ir.Phi {
		phis++
	}
	deferred := valueSet{} // operands of values with no location
	for j, in := range b.Instrs {
		if need(in) {
			s.take(in)
		}
		if in.Op == ir.Phi {
			// The φs are defined together: a dead one frees its cell only
			// once all of them have one.
			if j == phis-1 {
				for _, phi := range b.Instrs[:phis] {
					if !live(phi, j) {
						s.release(phi)
					}
				}
			}
			continue
		}
		if none[in] {
			for _, a := range in.Args {
				deferred[a] = true
			}
			continue
		}
		for _, a := range in.Args {
			if !live(a, j) && !deferred[a] {
				s.release(a)
			}
		}
		if need(in) && !live(in, j) {
			s.release(in)
		}
	}
}

// lastUse returns the index of the last instruction of the block b, not in
// SSA form, that uses each value.
func lastUse(b *ir.Block) map[*ir.Instr]int {
	last := map[*ir.Instr]int{}
	for j, in := range b.Instrs {
		for _, a := range in.Args {
			last[a] = j
		}
	}
	return last
}

// shareSlots returns the frame word of each slot of f and the number of
// words: one per slot, unless f is not in SSA form and has at most
// maxSlotColors slots, which are then colored by their interference, and
// unique is false.
func shareSlots(f *ir.Func, unique bool) ([]int, int) {
	ns := len(f.Slots)
	word := make([]int, ns)
	for i := range word {
		word[i] = i
	}
	if unique || f.SSA || ns < 2 || ns > maxSlotColors {
		return word, ns
	}
	w := (ns + 63) / 64
	n := len(f.Blocks)
	set := func(s []uint64, i int) { s[i/64] |= 1 << (i % 64) }
	has := func(s []uint64, i int) bool { return s[i/64]&(1<<(i%64)) != 0 }
	// gen: loaded before any store in the block; kill: stored in it.
	gen, kill := make([]uint64, n*w), make([]uint64, n*w)
	for _, b := range f.Blocks {
		g, k := gen[b.ID*w:(b.ID+1)*w], kill[b.ID*w:(b.ID+1)*w]
		for _, in := range b.Instrs {
			switch in.Op {
			case ir.Load:
				if !has(k, in.Slot.ID) {
					set(g, in.Slot.ID)
				}
			case ir.Store:
				set(k, in.Slot.ID)
			}
		}
	}
	in, out := make([]uint64, n*w), make([]uint64, n*w)
	for changed := true; changed; {
		changed = false
		for i := n - 1; i >= 0; i-- {
			b := f.Blocks[i]
			o := out[i*w : (i+1)*w]
			for _, t := range ir.Succs(b) {
				for k, x := range in[t.ID*w : (t.ID+1)*w] {
					o[k] |= x
				}
			}
			for k := range w {
				x := gen[i*w+k] | o[k]&^kill[i*w+k]
				if x != in[i*w+k] {
					in[i*w+k] = x
					changed = true
				}
			}
		}
	}
	adj := make([]uint64, ns*w)
	interfere := func(s int, live []uint64) {
		for k, x := range live {
			for x != 0 {
				t := k*64 + bits.TrailingZeros64(x)
				x &= x - 1
				if t != s {
					set(adj[s*w:(s+1)*w], t)
					set(adj[t*w:(t+1)*w], s)
				}
			}
		}
	}
	live := make([]uint64, w)
	for _, b := range f.Blocks {
		copy(live, out[b.ID*w:(b.ID+1)*w])
		for j := len(b.Instrs) - 1; j >= 0; j-- {
			switch x := b.Instrs[j]; x.Op {
			case ir.Store:
				interfere(x.Slot.ID, live)
				live[x.Slot.ID/64] &^= 1 << (x.Slot.ID % 64)
			case ir.Load:
				set(live, x.Slot.ID)
			}
		}
	}
	// Slots live into the entry, loaded only where nothing runs, are live
	// at once there.
	entry := in[f.Entry().ID*w : (f.Entry().ID+1)*w]
	for s := range ns {
		if has(entry, s) {
			interfere(s, entry)
		}
	}
	colors := 0
	taken := make([]bool, ns)
	for s := range ns {
		clear(taken[:colors])
		for t := range s {
			if has(adj[s*w:(s+1)*w], t) {
				taken[word[t]] = true
			}
		}
		c := slices.Index(taken[:colors], false)
		if c < 0 {
			c = colors
			colors++
		}
		word[s] = c
	}
	return word, colors
}

// checkCells verifies that the cells of plan and the slot words hold every
// value and slot wherever it is read: along every path from the entry to
// a read, the value's cells were last written by the value itself, and a
// slot's word by a store of that slot. The φs of a block must have
// distinct cells, which the edges into it write together.
func checkCells(f *ir.Func, plan cellPlan, word []int, words int) error {
	errf := func(format string, args ...any) error {
		return fmt.Errorf("amd64: func %s: cells: %s", f.Name, fmt.Sprintf(format, args...))
	}
	cellsOf := func(v *ir.Instr) (int, int, bool) {
		if c, ok := plan.tuples[v]; ok {
			return c, cellWidth(v), true
		}
		c, ok := plan.cells[v]
		return c, 1, ok
	}
	for _, v := range sortedValues(keys(plan.cells)) {
		if c := plan.cells[v]; c < 0 || c >= plan.n {
			return errf("v%d has cell %d of %d", v.ID, c, plan.n)
		}
	}
	for _, v := range sortedValues(keys(plan.tuples)) {
		if c := plan.tuples[v]; c < 0 || c+cellWidth(v) > plan.n {
			return errf("v%d has cells from %d of %d", v.ID, c, plan.n)
		}
	}
	for i, c := range word {
		if c < 0 || c >= words {
			return errf("slot %d has word %d of %d", i, c, words)
		}
	}
	// Cells shared across blocks need the dataflow below in an SSA
	// function: elsewhere a value lives in its block, whose cells start
	// empty. A larger SSA function's cells, and slots in words of their
	// own, are not shared.
	crossCells := f.SSA && countValues(f) <= maxAllocValues
	identity := true
	for i, c := range word {
		identity = identity && c == i
	}
	if identity {
		words = 0
	}
	if f.SSA && !crossCells {
		return nil
	}
	type state struct {
		cells []*ir.Instr
		slots []*ir.Slot
	}
	entry := map[*ir.Block]*state{f.Entry(): {make([]*ir.Instr, plan.n), make([]*ir.Slot, words)}}
	// meet keeps what every edge into b agrees on.
	meet := func(b *ir.Block, s state) bool {
		old, ok := entry[b]
		if !ok {
			entry[b] = &state{slices.Clone(s.cells), slices.Clone(s.slots)}
			return true
		}
		changed := false
		for i := range s.cells {
			if old.cells[i] != s.cells[i] && old.cells[i] != nil {
				old.cells[i], changed = nil, true
			}
		}
		for i := range s.slots {
			if old.slots[i] != s.slots[i] && old.slots[i] != nil {
				old.slots[i], changed = nil, true
			}
		}
		return changed
	}
	holds := func(s state, v *ir.Instr) bool {
		c, w, ok := cellsOf(v)
		if !ok {
			return true // a register or an immediate
		}
		for i := c; i < c+w; i++ {
			if s.cells[i] != v {
				return false
			}
		}
		return true
	}
	write := func(s state, v *ir.Instr) {
		if c, w, ok := cellsOf(v); ok {
			for i := c; i < c+w; i++ {
				s.cells[i] = v
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, b := range f.Blocks {
			s0, ok := entry[b]
			if !ok {
				continue
			}
			s := state{slices.Clone(s0.cells), slices.Clone(s0.slots)}
			if !crossCells {
				clear(s.cells)
			}
			for _, x := range b.Instrs {
				if x.Op == ir.Phi {
					continue
				}
				for _, a := range x.Args {
					if !holds(s, a) {
						return errf("%s reads v%d from a cell another value wrote", x, a.ID)
					}
				}
				switch {
				case words == 0:
				case x.Op == ir.Load:
					if s.slots[word[x.Slot.ID]] != x.Slot {
						return errf("%s reads slot %d from a word another slot wrote", x, x.Slot.ID)
					}
				case x.Op == ir.Store:
					s.slots[word[x.Slot.ID]] = x.Slot
				}
				write(s, x)
			}
			if len(b.Instrs) == 0 {
				continue
			}
			for k, t := range b.Instrs[len(b.Instrs)-1].Targets {
				edge := ir.Edge{From: b, Index: k}
				e := state{slices.Clone(s.cells), s.slots}
				var phis []int
				for _, phi := range t.Instrs {
					if phi.Op != ir.Phi {
						break
					}
					if a := phi.Args[indexOfEdge(phi, edge)]; !holds(s, a) {
						return errf("%s reads v%d along an edge from b%d from a cell another value wrote", phi, a.ID, b.ID)
					}
					if c, _, ok := cellsOf(phi); ok {
						if slices.Contains(phis, c) {
							return errf("%s shares cell %d with another φ of its block", phi, c)
						}
						phis = append(phis, c)
					}
					write(e, phi)
				}
				if meet(t, e) {
					changed = true
				}
			}
		}
	}
	return nil
}
