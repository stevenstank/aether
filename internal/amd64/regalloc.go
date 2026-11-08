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
	"math/bits"
	"slices"

	"github.com/stevenstank/aether/internal/ir"
)

// allocRegs are the allocatable registers, in preference order.
var allocRegs = [...]string{"%rbx", "%rsi", "%rdi", "%r8", "%r9", "%r10", "%r11", "%r12", "%r13", "%r14", "%r15"}

// maxAllocValues bounds the functions allocate takes on. Its working sets
// are bit sets of the function's values, two for each block and one for
// each instruction of the block being visited, so they still grow with the
// square of a function's size, which a program can make arbitrarily large:
// at the bound, they take a few MiB (TestAllocateScales). A larger function
// keeps every value in its cell, which costs speed and nothing else. It is
// well above the largest function of the S2 suite and of the workloads,
// which TestBuildBackendLimits checks.
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

// liveSets is the liveness of an SSA function's values: for each reachable
// block, the values live on entry, without the block's own φs, and the values
// live out of it, where a φ operand is live out of its edge's source block,
// not live into the φ's block. The sets are bit sets in which a value's bit
// is its rank in value-number order, so a set lists its values in that
// order.
//
// They are found by exploring paths backward from each use (Brandner et al.,
// "Computing Liveness Sets for SSA-Form Programs", 2011): a value used in a
// block other than its definition's is live into that block, and a value live
// into a block is live out of each predecessor, where it is live into the
// predecessor too unless it is defined there. A value enters a block's set at
// most once, so the work is the uses plus the size of the sets, with no
// iteration to a fixed point. Uses in unreachable blocks are ignored; the
// sets of reachable blocks do not depend on them.
type liveSets struct {
	index   map[*ir.Instr]int // a value's bit
	vals    []*ir.Instr       // the value of each bit
	w       int               // words per set
	in, out []uint64          // by block ID, w words each
}

func (l *liveSets) liveIn(b *ir.Block) []uint64  { return l.in[b.ID*l.w : (b.ID+1)*l.w] }
func (l *liveSets) liveOut(b *ir.Block) []uint64 { return l.out[b.ID*l.w : (b.ID+1)*l.w] }

// has reports whether v is in the set s.
func (l *liveSets) has(s []uint64, v *ir.Instr) bool {
	i, ok := l.index[v]
	return ok && s[i/64]&(1<<(i%64)) != 0
}

// each calls fn for the values of s in value-number order.
func (l *liveSets) each(s []uint64, fn func(v *ir.Instr)) {
	for k, word := range s {
		for word != 0 {
			i := bits.TrailingZeros64(word)
			fn(l.vals[k*64+i])
			word &= word - 1
		}
	}
}

// liveness computes the liveness of the SSA function f, whose dominator tree
// is d.
func liveness(f *ir.Func, d *ir.DomTree) *liveSets {
	l := &liveSets{index: map[*ir.Instr]int{}}
	block := map[*ir.Instr]int{}
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if in.Type != ir.NoType {
				l.vals = append(l.vals, in)
				block[in] = b.ID
			}
		}
	}
	slices.SortFunc(l.vals, func(x, y *ir.Instr) int { return x.ID - y.ID })
	def := make([]int, len(l.vals)) // each value's block, by bit
	for i, v := range l.vals {
		l.index[v] = i
		def[i] = block[v]
	}
	n := len(f.Blocks)
	l.w = (len(l.vals) + 63) / 64
	l.in, l.out = make([]uint64, n*l.w), make([]uint64, n*l.w)
	preds := make([][]int, n)
	for _, b := range f.Blocks {
		if d.Reachable(b) {
			for _, s := range ir.Succs(b) {
				preds[s.ID] = append(preds[s.ID], b.ID)
			}
		}
	}
	var work []int // blocks the value being explored is live into
	// liveOutOf marks the value of bit i live out of block p and, unless it
	// is defined there, queues p.
	liveOutOf := func(p, i int) {
		l.out[p*l.w+i/64] |= 1 << (i % 64)
		if def[i] != p && l.in[p*l.w+i/64]&(1<<(i%64)) == 0 {
			l.in[p*l.w+i/64] |= 1 << (i % 64)
			work = append(work, p)
		}
	}
	for _, b := range f.Blocks {
		if !d.Reachable(b) {
			continue
		}
		for _, in := range b.Instrs {
			for k, a := range in.Args {
				i, ok := l.index[a]
				if !ok {
					continue
				}
				switch {
				case in.Op == ir.Phi:
					liveOutOf(in.Edges[k].From.ID, i)
				case def[i] != b.ID && l.in[b.ID*l.w+i/64]&(1<<(i%64)) == 0:
					l.in[b.ID*l.w+i/64] |= 1 << (i % 64)
					work = append(work, b.ID)
				}
				for len(work) > 0 {
					x := work[len(work)-1]
					work = work[:len(work)-1]
					for _, p := range preds[x] {
						liveOutOf(p, i)
					}
				}
			}
		}
	}
	return l
}

// afterSets returns, for each instruction of the reachable block b, the
// values live after it, computed backward from those live out of b into buf,
// which it may reuse; the sets share buf. After a φ means after all of the
// block's φs, without the φs after it.
func (l *liveSets) afterSets(b *ir.Block, buf []uint64) ([][]uint64, []uint64) {
	k := len(b.Instrs)
	if cap(buf) < k*l.w {
		buf = make([]uint64, k*l.w)
	}
	buf = buf[:k*l.w]
	sets := make([][]uint64, k)
	live := slices.Clone(l.liveOut(b))
	for j := k - 1; j >= 0; j-- {
		x := b.Instrs[j]
		sets[j] = buf[j*l.w : (j+1)*l.w]
		copy(sets[j], live)
		if i, ok := l.index[x]; ok {
			live[i/64] &^= 1 << (i % 64)
		}
		if x.Op != ir.Phi {
			for _, a := range x.Args {
				if i, ok := l.index[a]; ok {
					live[i/64] |= 1 << (i % 64)
				}
			}
		}
	}
	return sets, buf
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
	lv := liveness(f, d)
	var buf []uint64
	cand := valueSet{}
	uses := map[*ir.Instr]int{}
	for _, b := range f.Blocks {
		if !d.Reachable(b) {
			continue
		}
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
		var after [][]uint64
		after, buf = lv.afterSets(b, buf)
		for j, in := range b.Instrs {
			if clobbers(in) {
				lv.each(after[j], func(v *ir.Instr) {
					if v != in {
						delete(cand, v)
					}
				})
			}
			if in.Op == ir.Println || in.Op == ir.Print {
				for _, a := range in.Args {
					delete(cand, a)
				}
			}
		}
	}
	buf = spill(f, d, lv, buf, cand, uses)
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
		lv.each(lv.liveIn(b), func(v *ir.Instr) {
			if r, ok := regs[v]; ok {
				used[r] = v
			}
		})
		var after [][]uint64
		after, buf = lv.afterSets(b, buf)
		phis := 0
		for phis < len(b.Instrs) && b.Instrs[phis].Op == ir.Phi {
			phis++
		}
		for j, in := range b.Instrs {
			if in.Op != ir.Phi {
				for _, a := range in.Args {
					if !lv.has(after[j], a) {
						release(a)
					}
				}
			}
			if cand[in] {
				if err := take(in); err != nil {
					return nil, err
				}
				if in.Op != ir.Phi && !lv.has(after[j], in) {
					release(in)
				}
			}
			// The φs are defined together, so a dead one frees its
			// register only once all of them have one.
			if j == phis-1 {
				for _, phi := range b.Instrs[:phis] {
					if !lv.has(after[j], phi) {
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
func spill(f *ir.Func, d *ir.DomTree, lv *liveSets, buf []uint64, cand valueSet, uses map[*ir.Instr]int) []uint64 {
	// The candidates as a bit set too, so that a point's pressure is a count
	// of bits and its candidates are listed only where there are too many.
	cs := make([]uint64, lv.w)
	for v := range cand {
		if i, ok := lv.index[v]; ok {
			cs[i/64] |= 1 << (i % 64)
		}
	}
	var live []*ir.Instr
	var def [1]*ir.Instr
	relieve := func(point []uint64, extra []*ir.Instr) {
		n := 0
		for k, word := range point {
			n += bits.OnesCount64(word & cs[k])
		}
		for _, v := range extra {
			if cand[v] && !lv.has(point, v) {
				n++
			}
		}
		if n <= len(allocRegs) {
			return
		}
		live = live[:0]
		lv.each(point, func(v *ir.Instr) {
			if cand[v] {
				live = append(live, v)
			}
		})
		for _, v := range extra {
			if cand[v] && !lv.has(point, v) {
				live = append(live, v)
			}
		}
		slices.SortFunc(live, func(x, y *ir.Instr) int {
			if uses[x] != uses[y] {
				return uses[x] - uses[y]
			}
			return x.ID - y.ID
		})
		for _, v := range live[:len(live)-len(allocRegs)] {
			delete(cand, v)
			if i, ok := lv.index[v]; ok {
				cs[i/64] &^= 1 << (i % 64)
			}
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
		// The entry: the values live into b and all of its φs.
		relieve(lv.liveIn(b), b.Instrs[:first])
		var after [][]uint64
		after, buf = lv.afterSets(b, buf)
		for j, in := range b.Instrs {
			if in.ID >= 0 && in.Op != ir.Phi {
				def[0] = in
				relieve(after[j], def[:])
			}
		}
	}
	return buf
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
