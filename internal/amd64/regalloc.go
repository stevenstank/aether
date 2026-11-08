package amd64

// Register allocation as designed by experiment RA-0 (D-026), which aether
// build uses (D-027).
//
// RA-0 changes only where values live. Instruction selection stays the
// accumulator sequences of instr, which read operands with movq or an
// arithmetic instruction and write the result last, so an operand may as
// well be a register as a cell. %rax, %rcx, and %rdx stay reserved scratch;
// values go in the 11 registers of allocRegs or keep their cells:
//   - only SSA functions are allocated, and only values of reachable blocks;
//   - a function of more than maxAllocValues values gets no registers;
//   - a value live across a call (an Aether call, println, print, or panic)
//     keeps its cell: under ABI-S0 (D-011) no register survives a call, and
//     the runtime helpers clobber %rdi and %rsi;
//   - an operand of println or print keeps its cell, since the helper
//     calls between operands clobber registers and the sequence loads each
//     operand into %rdi;
//   - a tuple keeps its result cells;
//   - where more values than registers would be live at once, the value
//     live there with the fewest uses keeps its cell (spill everywhere),
//     until no point needs more than len(allocRegs).
//
// The rest are colored greedily over the dominator tree in preorder: at
// each definition the value gets the first register no value live after it
// holds, or with EmitOptions.Coalesce, as aether build does, the free
// register that preferences ranks best, which saves moves. In SSA a value live at a point is live at every later definition
// that dominates the point, so the registers of the values live at once are
// distinct, and a free register always exists where no more than
// len(allocRegs) values are live. checkAllocation verifies the result
// independently.

import (
	"fmt"
	"slices"

	"github.com/stevenstank/aether/internal/ir"
)

// allocRegs are the allocatable registers, in preference order.
var allocRegs = [...]string{"%rbx", "%rsi", "%rdi", "%r8", "%r9", "%r10", "%r11", "%r12", "%r13", "%r14", "%r15"}

// maxAllocValues bounds the functions allocate takes on. Its working sets
// grow with the square of a function's values (a set of live values per
// instruction), which a program can make arbitrarily large: a println of a
// million constants needs about 10^11 entries. A larger function keeps
// every value in its cell, which costs speed and nothing else. It is well
// above the largest function of the S2 suite and of the workloads, which
// TestBuildBackendLimits checks.
const maxAllocValues = 2000

// valueSet is a set of values.
type valueSet map[*ir.Instr]bool

// clobbers reports whether in calls out of the function: values live across
// it cannot stay in registers.
func clobbers(in *ir.Instr) bool {
	switch in.Op {
	case ir.Call, ir.Println, ir.Print, ir.Panic:
		return true
	}
	return false
}

// liveness computes the values live out of each block of the SSA function
// f, by block ID. A φ operand is live out of its edge's source block, not
// live into the φ's block.
func liveness(f *ir.Func) []valueSet {
	n := len(f.Blocks)
	liveIn, liveOut := make([]valueSet, n), make([]valueSet, n)
	phiUses := make([]valueSet, n)
	for i := range n {
		liveIn[i], liveOut[i], phiUses[i] = valueSet{}, valueSet{}, valueSet{}
	}
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if in.Op == ir.Phi {
				for i, a := range in.Args {
					phiUses[in.Edges[i].From.ID][a] = true
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for i := n - 1; i >= 0; i-- {
			b := f.Blocks[i]
			out := valueSet{}
			for _, s := range ir.Succs(b) {
				for v := range liveIn[s.ID] {
					out[v] = true
				}
			}
			for v := range phiUses[b.ID] {
				out[v] = true
			}
			in := liveBefore(b, 0, out)
			// The sets only grow, so equal sizes mean equal sets.
			if len(out) != len(liveOut[b.ID]) || len(in) != len(liveIn[b.ID]) {
				changed = true
			}
			liveIn[b.ID], liveOut[b.ID] = in, out
		}
	}
	return liveOut
}

// liveBefore returns the values live before instruction i of b, given those
// live out of b. Before a φ means at the block's entry, where φs are
// defined and their operands are not used.
func liveBefore(b *ir.Block, i int, out valueSet) valueSet {
	live := valueSet{}
	for v := range out {
		live[v] = true
	}
	for j := len(b.Instrs) - 1; j >= i; j-- {
		x := b.Instrs[j]
		delete(live, x)
		if x.Op != ir.Phi {
			for _, a := range x.Args {
				live[a] = true
			}
		}
	}
	return live
}

// liveAfter returns, for each instruction of b, the values live after it.
func liveAfter(b *ir.Block, out valueSet) []valueSet {
	after := make([]valueSet, len(b.Instrs))
	live := valueSet{}
	for v := range out {
		live[v] = true
	}
	for j := len(b.Instrs) - 1; j >= 0; j-- {
		x := b.Instrs[j]
		after[j] = valueSet{}
		for v := range live {
			after[j][v] = true
		}
		delete(live, x)
		if x.Op != ir.Phi {
			for _, a := range x.Args {
				live[a] = true
			}
		}
	}
	return after
}

// allocate assigns registers, as indexes into allocRegs, to values of f; a
// value it does not assign keeps its cell. Functions not in SSA form get no
// registers.
func allocate(f *ir.Func) (map[*ir.Instr]int, error) { return allocateExcept(f, nil, false) }

// allocateExcept is allocate for a function whose values in none have no
// location at all (select.go). They take no register and count toward no
// point's pressure. With coalesce, a value takes the first free register
// that preferences gives it, if any, before the first free one.
func allocateExcept(f *ir.Func, none valueSet, coalesce bool) (map[*ir.Instr]int, error) {
	regs := map[*ir.Instr]int{}
	if !f.SSA || countValues(f) > maxAllocValues {
		return regs, nil
	}
	d := ir.Dominators(f)
	liveOut := liveness(f)
	after := make([][]valueSet, len(f.Blocks))
	cand := valueSet{}
	uses := map[*ir.Instr]int{}
	for _, b := range f.Blocks {
		if !d.Reachable(b) {
			continue
		}
		after[b.ID] = liveAfter(b, liveOut[b.ID])
		for _, in := range b.Instrs {
			if in.ID >= 0 && in.Type != ir.Tuple && !none[in] {
				cand[in] = true
			}
			for _, a := range in.Args {
				uses[a]++
			}
		}
	}
	for _, b := range f.Blocks {
		if !d.Reachable(b) {
			continue
		}
		for j, in := range b.Instrs {
			if clobbers(in) {
				for v := range after[b.ID][j] {
					if v != in {
						delete(cand, v)
					}
				}
			}
			if in.Op == ir.Println || in.Op == ir.Print {
				for _, a := range in.Args {
					delete(cand, a)
				}
			}
		}
	}
	spill(f, d, liveOut, after, cand, uses)
	var prefer func(v *ir.Instr) []int
	if coalesce {
		prefer = preferences(f, regs)
	}
	// Color in dominator-tree preorder.
	work := []*ir.Block{f.Entry()}
	for len(work) > 0 {
		b := work[len(work)-1]
		work = work[:len(work)-1]
		var used [len(allocRegs)]*ir.Instr
		take := func(v *ir.Instr) error {
			if prefer != nil {
				for _, r := range prefer(v) {
					if used[r] == nil {
						used[r], regs[v] = v, r
						return nil
					}
				}
			}
			for r := range used {
				if used[r] == nil {
					used[r], regs[v] = v, r
					return nil
				}
			}
			return fmt.Errorf("amd64: func %s: no register free for v%d", f.Name, v.ID)
		}
		release := func(v *ir.Instr) {
			if r, ok := regs[v]; ok && used[r] == v {
				used[r] = nil
			}
		}
		// The values live into b that have registers already hold them:
		// their definitions dominate b, so they were colored first.
		for _, v := range sortedValues(liveBefore(b, 0, liveOut[b.ID])) {
			if r, ok := regs[v]; ok {
				used[r] = v
			}
		}
		phis := 0
		for phis < len(b.Instrs) && b.Instrs[phis].Op == ir.Phi {
			phis++
		}
		for j, in := range b.Instrs {
			if in.Op != ir.Phi {
				for _, a := range in.Args {
					if !after[b.ID][j][a] {
						release(a)
					}
				}
			}
			if cand[in] {
				if err := take(in); err != nil {
					return nil, err
				}
				if in.Op != ir.Phi && !after[b.ID][j][in] {
					release(in)
				}
			}
			// The φs are defined together, so a dead one frees its
			// register only once all of them have one.
			if j == phis-1 {
				for _, phi := range b.Instrs[:phis] {
					if !after[b.ID][j][phi] {
						release(phi)
					}
				}
			}
		}
		kids := d.Children(b)
		for i := len(kids) - 1; i >= 0; i-- {
			work = append(work, kids[i])
		}
	}
	return regs, nil
}

// preferences returns, for a value about to be colored, the registers that
// would make moves of the emitted code unnecessary, given regs, the registers
// of the values colored so far. Each value related to it as below votes for
// its register, and the registers come most votes first, then in the order
// of the list:
//   - a φ that the value is an operand of (3 votes): the copy on the edge
//     becomes a move to itself, which edgeCopies drops. Coloring follows the
//     dominator tree, so that φ is colored first only when its block
//     dominates the value's: the edge closes a loop.
//   - an operand of a φ (2 votes), and the φ's other operands (1 vote), for
//     the same reason;
//   - for a param among the first len(argRegs), ABI-R1's register of its
//     argument (2 votes), which makes the entry copy's move unnecessary;
//   - for an argument of a call, each register ABI-R1 passes it in (2
//     votes);
//   - for the result of an operation computed in place from its first
//     operand (twoAddress, selectShift, and Not), that operand (2 votes),
//     which then needs no move.
//
// Only a free register is ever taken, so the coloring stays correct
// whatever the preferences are; they only choose among correct colorings,
// and checkAllocation checks the result as before. Under ABI-S0 the
// argument registers are merely registers like the others.
func preferences(f *ir.Func, regs map[*ir.Instr]int) func(v *ir.Instr) []int {
	phisOf := map[*ir.Instr][]*ir.Instr{} // the φs v is an operand of, in order
	argOf := map[*ir.Instr][]int{}        // the argument positions v is passed in
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			switch in.Op {
			case ir.Phi:
				for _, a := range in.Args {
					if !slices.Contains(phisOf[a], in) {
						phisOf[a] = append(phisOf[a], in)
					}
				}
			case ir.Call:
				for i, a := range in.Args {
					if i < len(argRegs) {
						argOf[a] = append(argOf[a], i)
					}
				}
			}
		}
	}
	argReg := func(i int) int { return slices.Index(allocRegs[:], argRegs[i]) }
	return func(v *ir.Instr) []int {
		var out []int
		votes := map[int]int{}
		vote := func(r, w int) {
			if votes[r] == 0 {
				out = append(out, r)
			}
			votes[r] += w
		}
		add := func(x *ir.Instr, w int) {
			if r, ok := regs[x]; ok && x != v {
				vote(r, w)
			}
		}
		for _, p := range phisOf[v] {
			add(p, 3)
		}
		if v.Op == ir.Phi {
			for _, a := range v.Args {
				add(a, 2)
			}
		}
		for _, p := range phisOf[v] {
			for _, a := range p.Args {
				add(a, 1)
			}
		}
		if v.Op == ir.Param && v.Index < len(argRegs) {
			vote(argReg(v.Index), 2)
		}
		for _, i := range argOf[v] {
			vote(argReg(i), 2)
		}
		switch v.Op {
		case ir.Add, ir.Sub, ir.Mul, ir.And, ir.Or, ir.Xor, ir.AndNot, ir.Neg, ir.Com, ir.Conv, ir.Shl, ir.Shr, ir.Not:
			add(v.Args[0], 2)
		}
		slices.SortStableFunc(out, func(x, y int) int { return votes[y] - votes[x] })
		return out
	}
}

// countValues returns the number of values f defines.
func countValues(f *ir.Func) (n int) {
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if in.ID >= 0 {
				n++
			}
		}
	}
	return n
}

// spill removes values from cand (spill everywhere) until no point of f has
// more candidates live than there are registers. It visits the points in
// order: each block's entry, with all of its φs, and each definition, with
// the values live after it. Where too many are live, those with the fewest
// uses, and of those the ones defined first, keep their cells. Removing
// candidates only lowers the pressure at points already visited, so one
// pass is enough.
func spill(f *ir.Func, d *ir.DomTree, liveOut []valueSet, after [][]valueSet, cand valueSet, uses map[*ir.Instr]int) {
	relieve := func(point valueSet, def *ir.Instr) {
		var live []*ir.Instr
		for v := range point {
			if cand[v] {
				live = append(live, v)
			}
		}
		if def != nil && cand[def] && !point[def] {
			live = append(live, def)
		}
		if len(live) <= len(allocRegs) {
			return
		}
		slices.SortFunc(live, func(x, y *ir.Instr) int {
			if uses[x] != uses[y] {
				return uses[x] - uses[y]
			}
			return x.ID - y.ID
		})
		for _, v := range live[:len(live)-len(allocRegs)] {
			delete(cand, v)
		}
	}
	for _, b := range f.Blocks {
		if !d.Reachable(b) {
			continue
		}
		first := 0
		for first < len(b.Instrs) && b.Instrs[first].Op == ir.Phi {
			first++
		}
		entry := liveBefore(b, first, liveOut[b.ID])
		for _, phi := range b.Instrs[:first] {
			entry[phi] = true
		}
		relieve(entry, nil)
		for j, in := range b.Instrs {
			if in.ID >= 0 && in.Op != ir.Phi {
				relieve(after[b.ID][j], in)
			}
		}
	}
}

// sortedValues returns s in value-number order.
func sortedValues(s valueSet) []*ir.Instr {
	vs := make([]*ir.Instr, 0, len(s))
	for v := range s {
		vs = append(vs, v)
	}
	slices.SortFunc(vs, func(x, y *ir.Instr) int { return x.ID - y.ID })
	return vs
}

// checkAllocation verifies regs, an allocation for f, independently of how
// allocate found it. It tracks which value each register holds at each
// point of the reachable code, as the emitted code changes them: a
// definition writes its register, a φ copy on an edge writes the φ's
// register with the operand read before any copy on the edge, and a call
// leaves every register unknown. Every use of a value with a register
// must find the value there on every path. Operands of println and print,
// tuples, and values of unreachable blocks must have no register.
func checkAllocation(f *ir.Func, regs map[*ir.Instr]int) error {
	errf := func(format string, args ...any) error {
		return fmt.Errorf("amd64: func %s: allocation: %s", f.Name, fmt.Sprintf(format, args...))
	}
	d := ir.Dominators(f)
	reachable := map[*ir.Instr]bool{}
	for _, b := range f.Blocks {
		for _, x := range b.Instrs {
			reachable[x] = d.Reachable(b)
		}
	}
	for _, v := range sortedValues(keys(regs)) {
		r := regs[v]
		switch {
		case r < 0 || r >= len(allocRegs):
			return errf("v%d has no register %d", v.ID, r)
		case v.ID < 0 || v.Type == ir.Tuple:
			return errf("%s cannot have a register", v)
		case !reachable[v]:
			return errf("v%d is not in a reachable block of the function", v.ID)
		}
	}
	type state = [len(allocRegs)]*ir.Instr
	entry := map[*ir.Block]*state{f.Entry(): {}}
	// meet keeps what every incoming edge agrees on.
	meet := func(b *ir.Block, s state) bool {
		old, ok := entry[b]
		if !ok {
			entry[b] = &s
			return true
		}
		changed := false
		for r := range s {
			if old[r] != s[r] && old[r] != nil {
				old[r] = nil
				changed = true
			}
		}
		return changed
	}
	order := reversePostorder(f, d)
	for pass, changed := 0, true; changed; pass++ {
		changed = false
		for _, b := range order {
			s0, ok := entry[b]
			if !ok {
				continue
			}
			s := *s0
			for _, x := range b.Instrs {
				if x.Op == ir.Phi {
					continue
				}
				for _, a := range x.Args {
					r, ok := regs[a]
					switch {
					case ok && (x.Op == ir.Println || x.Op == ir.Print):
						return errf("%s has operand v%d in %s", x, a.ID, allocRegs[r])
					case ok && s[r] != a:
						return errf("%s reads v%d from %s, which holds %s", x, a.ID, allocRegs[r], holder(s[r]))
					}
				}
				if clobbers(x) {
					s = state{}
				}
				if r, ok := regs[x]; ok {
					s[r] = x
				}
			}
			for i, t := range ir.Succs(b) {
				edge := ir.Edge{From: b, Index: i}
				next := s
				for _, phi := range t.Instrs {
					if phi.Op != ir.Phi {
						break
					}
					a := phi.Args[slices.Index(phi.Edges, edge)]
					if r, ok := regs[a]; ok && s[r] != a {
						return errf("the edge b%d.%d to b%d copies v%d from %s, which holds %s", b.ID, i, t.ID, a.ID, allocRegs[r], holder(s[r]))
					}
					if r, ok := regs[phi]; ok {
						next[r] = phi
					}
				}
				if meet(t, next) {
					changed = true
				}
			}
		}
		if pass > len(f.Blocks)*(len(allocRegs)+1) {
			return errf("no fixed point")
		}
	}
	return nil
}

func holder(v *ir.Instr) string {
	if v == nil {
		return "no known value"
	}
	return fmt.Sprintf("v%d", v.ID)
}

func keys(m map[*ir.Instr]int) valueSet {
	s := valueSet{}
	for v := range m {
		s[v] = true
	}
	return s
}

// reversePostorder returns the reachable blocks of f in reverse postorder
// of a depth-first walk along successor edges.
func reversePostorder(f *ir.Func, d *ir.DomTree) []*ir.Block {
	seen := make([]bool, len(f.Blocks))
	var post []*ir.Block
	var walk func(b *ir.Block)
	walk = func(b *ir.Block) {
		seen[b.ID] = true
		for _, s := range ir.Succs(b) {
			if !seen[s.ID] {
				walk(s)
			}
		}
		post = append(post, b)
	}
	walk(f.Entry())
	slices.Reverse(post)
	return post
}
