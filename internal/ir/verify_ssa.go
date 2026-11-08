package ir

import "slices"

// This file holds the verifier's checks for functions in SSA form (D-025).
// They run in addition to the checks every function gets, which already
// reject slots' loads and stores in SSA form, phis in other functions, and
// operand type mismatches.

// ssa prepares the checks of an SSA function's instructions. It records
// the block defining each instruction and rejects an instruction that
// appears twice. If the CFG can be analyzed, it computes the dominator
// tree and each block's incoming edges, and rejects edges into the entry.
func (v *verifier) ssa(reachable []bool) {
	v.reach = reachable
	v.def = map[*Instr]int{}
	v.ids = map[int]bool{}
	cfgOK := true
	for bi, b := range v.f.Blocks {
		for i, in := range b.Instrs {
			if in == nil {
				continue
			}
			if _, dup := v.def[in]; dup {
				v.errorf(bi, i, in, "instruction appears more than once in the function")
			}
			v.def[in] = bi
		}
		// Dominators follows the last instruction's targets, so they must
		// be blocks of the function, and only jump and branch may have
		// any (otherwise reported by signature).
		if len(b.Instrs) == 0 {
			continue
		}
		t := b.Instrs[len(b.Instrs)-1]
		if t == nil || t.Op != Jump && t.Op != Branch && len(t.Targets) > 0 {
			cfgOK = false
			continue
		}
		for _, target := range t.Targets {
			cfgOK = cfgOK && v.isBlock(target)
		}
	}
	if !cfgOK {
		return
	}
	v.dom = Dominators(v.f)
	v.incoming = make([][]Edge, len(v.f.Blocks))
	v.edgeIndex = make([]map[Edge]int, len(v.f.Blocks))
	for _, b := range v.f.Blocks {
		if !reachable[b.ID] {
			continue
		}
		for k, s := range Succs(b) {
			if v.edgeIndex[s.ID] == nil {
				v.edgeIndex[s.ID] = map[Edge]int{}
			}
			v.edgeIndex[s.ID][Edge{b, k}] = len(v.incoming[s.ID])
			v.incoming[s.ID] = append(v.incoming[s.ID], Edge{b, k})
		}
	}
	for _, e := range v.incoming[0] {
		v.errorf(0, -1, nil, "edge %s enters the entry block of a function in SSA form", e)
	}
}

// dominatingDef checks operand a of in, the instruction at index i of
// block bi, where a is not defined earlier in that block: it must be
// defined in a block that strictly dominates bi. A tuple must be used in
// the block of its call.
func (v *verifier) dominatingDef(bi, i int, in, a *Instr) {
	db, ok := v.def[a]
	switch {
	case !ok:
		v.errorf(bi, i, in, "operand v%d is not an instruction of this function", a.ID)
	case db == bi:
		v.errorf(bi, i, in, "operand v%d is not defined before its use in this block", a.ID)
	case a.Type == Tuple:
		v.errorf(bi, i, in, "tuple v%d used outside the block of its call", a.ID)
	case v.dom == nil || !v.reach[bi]:
		// The CFG or this block is already reported as malformed.
	case !v.dom.Dominates(v.f.Blocks[db], v.f.Blocks[bi]):
		v.errorf(bi, i, in, "operand v%d is defined in b%d, which does not dominate b%d", a.ID, db, bi)
	}
}

// phi checks the incoming edges and values of in, a phi at index i of
// block bi. Its edges must be exactly the block's incoming edges from
// reachable blocks, each once, in canonical order: by the source block's
// ID, then by target index. The value for each edge must be defined in a
// block that dominates the edge's source, so it is available at the end
// of the source block on every path.
func (v *verifier) phi(bi, i int, in *Instr) {
	if v.dom == nil || !v.reach[bi] {
		return
	}
	want := v.incoming[bi]
	seen := make([]bool, len(want)) // by position in want
	ok := true
	for _, e := range in.Edges {
		k, incoming := v.edgeIndex[bi][e]
		switch {
		case !incoming:
			v.errorf(bi, i, in, "edge %s is not an incoming edge of b%d", e, bi)
		case seen[k]:
			v.errorf(bi, i, in, "edge %s has more than one value", e)
		default:
			seen[k] = true
			continue
		}
		ok = false
	}
	for k, e := range want {
		if !seen[k] {
			v.errorf(bi, i, in, "no value for incoming edge %s", e)
			ok = false
		}
	}
	if ok && !slices.Equal(in.Edges, want) {
		v.errorf(bi, i, in, "incoming edges are not in canonical order (by block, then target index)")
	}
	for j, e := range in.Edges {
		if k, incoming := v.edgeIndex[bi][e]; j >= len(in.Args) || in.Args[j] == nil || !incoming || !seen[k] {
			continue // reported above or by signature
		}
		a := in.Args[j]
		db, defined := v.def[a]
		switch {
		case !defined:
			v.errorf(bi, i, in, "value v%d for edge %s is not an instruction of this function", a.ID, e)
		case !v.dom.Dominates(v.f.Blocks[db], e.From):
			v.errorf(bi, i, in, "value v%d for edge %s is defined in b%d, which does not dominate b%d",
				a.ID, e, db, e.From.ID)
		}
	}
}
