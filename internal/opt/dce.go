package opt

import (
	"errors"
	"slices"

	"github.com/stevenstank/aether/internal/ir"
)

// DCE removes the dead instructions of f, which must be in SSA form and well
// formed, as Verify checks; then the result is too. It reports whether it
// changed f.
//
// An instruction is live if it has an effect or is an operand of a live
// instruction. The instructions with an effect are the terminators, Call,
// Println, Print, GStore, StM, and Param, which ABI-R1's entry accounts for
// (D-030). Everything else computes a value and nothing more: Div and Rem
// cannot trap, since lowering's check for a zero divisor is a branch of its
// own, and a GLoad's or LdM's only effect is its value. Dead phis that use each other
// are removed together. Live instructions are neither removed nor moved, so
// calls, panics, and global and memory accesses keep their order (D-024).
func DCE(f *ir.Func) (bool, error) {
	if !f.SSA {
		return false, errors.New("dce: func " + f.Name + " is not in SSA form")
	}
	live := map[*ir.Instr]bool{}
	var work []*ir.Instr
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if hasEffect(in) {
				live[in] = true
				work = append(work, in)
			}
		}
	}
	for len(work) > 0 {
		in := work[len(work)-1]
		work = work[:len(work)-1]
		for _, a := range in.Args {
			if !live[a] {
				live[a] = true
				work = append(work, a)
			}
		}
	}
	changed := false
	for _, b := range f.Blocks {
		n := len(b.Instrs)
		b.Instrs = slices.DeleteFunc(b.Instrs, func(in *ir.Instr) bool { return !live[in] })
		changed = changed || len(b.Instrs) != n
	}
	return changed, nil
}

// hasEffect reports whether in does more than compute its value.
func hasEffect(in *ir.Instr) bool {
	switch in.Op {
	case ir.Call, ir.Println, ir.Print, ir.GStore, ir.StM, ir.Param:
		return true
	}
	return in.Op.IsTerminator()
}
