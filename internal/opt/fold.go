package opt

import (
	"errors"
	"slices"

	"github.com/stevenstank/aether/internal/ir"
)

// Fold performs constant folding on f, which must be in SSA form and well
// formed, as Verify checks; then the result is too. It reports whether it
// changed f.
//
//   - An instruction whose operands are all constants and whose result
//     FoldValue defines becomes a Const of that result, keeping its value
//     number and position, so its uses need no change.
//   - A Branch on a constant becomes a Jump to the target it would take. The
//     blocks no longer reachable from the entry are removed, with the
//     incoming values of their edges, and the remaining blocks renumbered in
//     order.
//   - A phi with a single incoming value, or the same value along every edge
//     but those from itself, is replaced by that value.
//   - Constants with no uses are removed.
//
// These repeat until none applies. Only blocks that can never execute lose
// code with an effect, and nothing is moved, so calls, panics, and global
// accesses keep their order (D-024). A division by a constant zero is not
// folded: lowering's check before it panics first. Fold returns an error,
// with f perhaps partly folded, only if f is not in SSA form or not well
// formed.
func Fold(f *ir.Func) (bool, error) {
	if !f.SSA {
		return false, errors.New("fold: func " + f.Name + " is not in SSA form")
	}
	changed := false
	for first := true; ; first = false {
		c := foldValues(f)
		c = foldBranches(f, first) || c
		p, err := foldPhis(f)
		if err != nil {
			return false, err
		}
		if !c && !p {
			break
		}
		changed = true
	}
	return removeDeadConsts(f) || changed, nil
}

// foldValues turns each instruction with a constant result into a Const.
func foldValues(f *ir.Func) bool {
	changed := false
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if v, ok := FoldValue(in); ok {
				in.Op, in.Const, in.Args = ir.Const, v, nil
				changed = true
			}
		}
	}
	return changed
}

// FoldValue returns the canonical result of in if in is a pure operation
// whose operands are all constants and the result is defined: Add through
// Shr, Conv, the comparisons, and Not; a Div or Rem only by a nonzero
// divisor.
func FoldValue(in *ir.Instr) (int64, bool) {
	if len(in.Args) == 0 {
		return 0, false
	}
	for _, a := range in.Args {
		if a.Op != ir.Const {
			return 0, false
		}
	}
	x := in.Args[0].Const
	var y int64
	if len(in.Args) > 1 {
		y = in.Args[1].Const
	}
	t := in.Type
	b := func(c bool) (int64, bool) {
		if c {
			return 1, true
		}
		return 0, true
	}
	switch in.Op {
	case ir.Add:
		return t.Canonical(x + y), true
	case ir.Sub:
		return t.Canonical(x - y), true
	case ir.Mul:
		return t.Canonical(x * y), true
	case ir.And:
		return x & y, true
	case ir.Or:
		return x | y, true
	case ir.Xor:
		return x ^ y, true
	case ir.AndNot:
		return x &^ y, true
	case ir.Neg:
		return t.Canonical(-x), true
	case ir.Com:
		return t.Canonical(^x), true
	case ir.Not:
		return 1 - x, true
	case ir.Conv:
		return t.Canonical(x), true
	case ir.Div, ir.Rem:
		switch {
		case y == 0:
			return 0, false
		case t.Signed() && y == -1 && in.Op == ir.Div:
			return t.Canonical(-x), true
		case t.Signed() && y == -1:
			return 0, true
		case t.Signed() && in.Op == ir.Div:
			return t.Canonical(x / y), true
		case t.Signed():
			return t.Canonical(x % y), true
		case in.Op == ir.Div:
			return t.Canonical(int64(uint64(x) / uint64(y))), true
		}
		return t.Canonical(int64(uint64(x) % uint64(y))), true
	case ir.Shl, ir.Shr:
		// The count is unsigned, so its canonical form is its value.
		n, w := uint64(y), uint64(in.Args[0].Type.Width())
		switch {
		case n >= w && (in.Op == ir.Shl || !t.Signed() || x >= 0):
			return 0, true
		case n >= w:
			return -1, true
		case in.Op == ir.Shl:
			return t.Canonical(x << n), true
		case t.Signed():
			return x >> n, true
		}
		return int64(uint64(x) >> n), true
	case ir.Eq:
		return b(x == y)
	case ir.Ne:
		return b(x != y)
	}
	if !in.Op.IsComparison() {
		return 0, false
	}
	c := compareInts(x, y, in.Args[0].Type.IsInteger() && !in.Args[0].Type.Signed())
	switch in.Op {
	case ir.Lt:
		return b(c < 0)
	case ir.Le:
		return b(c <= 0)
	case ir.Gt:
		return b(c > 0)
	}
	return b(c >= 0)
}

// compareInts compares canonical values, as unsigned numbers if unsigned.
func compareInts(x, y int64, unsigned bool) int {
	if unsigned {
		ux, uy := uint64(x), uint64(y)
		switch {
		case ux < uy:
			return -1
		case ux > uy:
			return 1
		}
		return 0
	}
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// foldBranches turns Branches on constants into Jumps, removes the blocks
// that are then unreachable, and rebuilds every phi's edges from the new
// CFG. Only folded branches make blocks unreachable, so unless first, a call
// that folds none changes nothing.
func foldBranches(f *ir.Func, first bool) bool {
	type oldEdge struct {
		from  *ir.Block
		index int
	}
	// For each phi, its value along each edge, keyed by the edge as it was.
	vals := map[*ir.Instr]map[oldEdge]*ir.Instr{}
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if in.Op != ir.Phi {
				break
			}
			m := map[oldEdge]*ir.Instr{}
			for i, e := range in.Edges {
				m[oldEdge{e.From, e.Index}] = in.Args[i]
			}
			vals[in] = m
		}
	}
	// kept[b] is the old index of the target a folded Branch of b keeps.
	kept := map[*ir.Block]int{}
	for _, b := range f.Blocks {
		t := b.Instrs[len(b.Instrs)-1]
		if t.Op != ir.Branch || t.Args[0].Op != ir.Const {
			continue
		}
		k := 1 - int(t.Args[0].Const)
		t.Op, t.Args, t.Targets = ir.Jump, nil, []*ir.Block{t.Targets[k]}
		kept[b] = k
	}
	if len(kept) == 0 && !first {
		return false
	}
	d := ir.Dominators(f)
	removed := false
	var blocks []*ir.Block
	for _, b := range f.Blocks {
		if d.Reachable(b) {
			b.ID = len(blocks)
			blocks = append(blocks, b)
		} else {
			removed = true
		}
	}
	if len(kept) == 0 && !removed {
		return false
	}
	f.Blocks = blocks
	incoming := make([][]ir.Edge, len(blocks))
	for _, b := range blocks {
		for k, t := range ir.Succs(b) {
			incoming[t.ID] = append(incoming[t.ID], ir.Edge{From: b, Index: k})
		}
	}
	for _, b := range blocks {
		for _, in := range b.Instrs {
			if in.Op != ir.Phi {
				break
			}
			in.Edges, in.Args = nil, nil
			for _, e := range incoming[b.ID] {
				old := oldEdge{e.From, e.Index}
				if k, ok := kept[e.From]; ok {
					old.index = k
				}
				in.AddIncoming(e, vals[in][old])
			}
		}
	}
	return true
}

// foldPhis replaces each phi whose value is the same along every edge, but
// those from itself, by that value, and turns each phi whose values are
// constants all equal to c into a Const of c, placed after the block's phis.
func foldPhis(f *ir.Func) (bool, error) {
	repl := map[*ir.Instr]*ir.Instr{}
	changed := false
	for _, b := range f.Blocks {
		consts := map[*ir.Instr]int64{}
		var order []*ir.Instr
		for _, in := range b.Instrs {
			if in.Op != ir.Phi {
				break
			}
			var v *ir.Instr // the first value other than the phi itself
			trivial, constant := true, true
			for _, a := range in.Args {
				if a == in {
					continue
				}
				if v == nil {
					v = a
				}
				trivial = trivial && a == v
				constant = constant && a.Op == ir.Const && a.Const == v.Const
			}
			switch {
			case v == nil:
			case trivial:
				repl[in] = v
			case constant:
				consts[in] = v.Const
				order = append(order, in)
			}
		}
		if len(order) == 0 {
			continue
		}
		changed = true
		b.Instrs = slices.DeleteFunc(b.Instrs, func(in *ir.Instr) bool { _, ok := consts[in]; return ok })
		at := 0
		for at < len(b.Instrs) && b.Instrs[at].Op == ir.Phi {
			at++
		}
		for _, in := range order {
			in.Op, in.Const, in.Args, in.Edges = ir.Const, consts[in], nil, nil
		}
		b.Instrs = slices.Insert(b.Instrs, at, order...)
	}
	if len(repl) == 0 {
		return changed, nil
	}
	// A phi may be replaced by another replaced phi. A cycle of them would
	// have no value from the entry, which Verify excludes.
	cycle := false
	final := func(v *ir.Instr) *ir.Instr {
		for range len(repl) + 1 {
			r, ok := repl[v]
			if !ok {
				return v
			}
			v = r
		}
		cycle = true
		return v
	}
	for _, b := range f.Blocks {
		b.Instrs = slices.DeleteFunc(b.Instrs, func(in *ir.Instr) bool { _, ok := repl[in]; return ok })
		for _, in := range b.Instrs {
			for i, a := range in.Args {
				in.Args[i] = final(a)
			}
		}
	}
	if cycle {
		return false, errors.New("fold: func " + f.Name + ": a cycle of phis with no other value")
	}
	return true, nil
}

// removeDeadConsts removes the constants that nothing uses.
func removeDeadConsts(f *ir.Func) bool {
	used := map[*ir.Instr]bool{}
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			for _, a := range in.Args {
				used[a] = true
			}
		}
	}
	changed := false
	for _, b := range f.Blocks {
		n := len(b.Instrs)
		b.Instrs = slices.DeleteFunc(b.Instrs, func(in *ir.Instr) bool { return in.Op == ir.Const && !used[in] })
		changed = changed || len(b.Instrs) != n
	}
	return changed
}
