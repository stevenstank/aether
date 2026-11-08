package ir

import "fmt"

// VerifyError is one violated invariant. It names the function, and the
// block and instruction where applicable, so a failure points at the
// malformed IR rather than at the user's source.
type VerifyError struct {
	Func  string
	Block int // -1 if not about a block
	Index int // instruction index in the block; -1 if not about one
	Instr string
	Msg   string
}

func (e *VerifyError) Error() string {
	s := "func " + e.Func
	if e.Block >= 0 {
		s += fmt.Sprintf(": b%d", e.Block)
	}
	if e.Index >= 0 {
		s += fmt.Sprintf(": instr %d (%s)", e.Index, e.Instr)
	}
	return s + ": " + e.Msg
}

// Verify checks the invariants of docs/ARCHITECTURE.md §4 that apply to
// this IR and returns every violation found, in a deterministic order. An
// empty result means p is well formed. Lowering must only ever produce
// well-formed IR, so any error is an internal compiler error.
func Verify(p *Program) []error {
	errs, funcs, globals := verifyProgram(p)
	names := map[string]bool{}
	mains := 0
	for _, f := range p.Funcs {
		if f == nil {
			errs = append(errs, &VerifyError{Func: "?", Block: -1, Index: -1, Msg: "nil function"})
			continue
		}
		if names[f.Name] {
			errs = append(errs, &VerifyError{Func: f.Name, Block: -1, Index: -1, Msg: "duplicate function name"})
		}
		names[f.Name] = true
		if f.Name == "main" {
			mains++
		}
		errs = append(errs, verifyFunc(p, f, funcs, globals)...)
	}
	if mains == 0 {
		errs = append(errs, &VerifyError{Func: "main", Block: -1, Index: -1, Msg: "missing entry point: no function named main"})
	}
	return errs
}

// VerifyFuncs checks fs, functions of p, as Verify checks every function,
// and returns the violations found. It is for a program that Verify accepted
// and whose other functions, globals, and function list have not changed
// since: then the result is empty exactly when Verify's would be. Program
// errors that Verify reports are not reported again.
func VerifyFuncs(p *Program, fs []*Func) []error {
	_, funcs, globals := verifyProgram(p)
	var errs []error
	for _, f := range fs {
		if !funcs[f] {
			errs = append(errs, &VerifyError{Func: f.Name, Block: -1, Index: -1, Msg: "not a function of the program"})
			continue
		}
		errs = append(errs, verifyFunc(p, f, funcs, globals)...)
	}
	return errs
}

// verifyProgram checks p's globals and Init, and returns the violations, the
// functions of p, and its well-formed globals.
func verifyProgram(p *Program) ([]error, map[*Func]bool, map[*Global]bool) {
	var errs []error
	funcs := map[*Func]bool{}
	for _, f := range p.Funcs {
		funcs[f] = true
	}
	globals := map[*Global]bool{}
	gnames := map[string]bool{}
	for i, g := range p.Globals {
		e := &VerifyError{Func: "?", Block: -1, Index: -1}
		switch {
		case g == nil:
			e.Msg = fmt.Sprintf("global %d is nil", i)
		case g.ID != i:
			e.Msg = fmt.Sprintf("global %d has ID %d", i, g.ID)
		case gnames[g.Name]:
			e.Msg = fmt.Sprintf("duplicate global name %s", g.Name)
		case !validType(g.Type):
			e.Msg = fmt.Sprintf("global g%d has invalid type %s", i, g.Type)
		case g.File == "" || !validPos(g.Pos):
			e.Msg = fmt.Sprintf("global g%d has no source position", i)
		default:
			globals[g] = true
			gnames[g.Name] = true
			continue
		}
		errs = append(errs, e)
	}
	if p.Init != nil && !funcs[p.Init] {
		errs = append(errs, &VerifyError{Func: p.Init.Name, Block: -1, Index: -1, Msg: "Init is not a function of the program"})
	}
	return errs, funcs, globals
}

type verifier struct {
	p       *Program
	f       *Func
	funcs   map[*Func]bool   // the functions of the program
	globals map[*Global]bool // its well-formed globals
	errs    []error

	unstored map[*Instr]bool // the loads not stored on every path, by unstoredLoads

	// For a function in SSA form (D-025), set by ssa.
	reach     []bool
	def       map[*Instr]int // the block of each instruction
	ids       map[int]bool   // value numbers seen, in the whole function
	dom       *DomTree       // nil if the CFG is too malformed to analyze
	incoming  [][]Edge       // each block's edges from reachable blocks, in canonical order
	edgeIndex []map[Edge]int // each edge's position in incoming
}

func (v *verifier) errorf(block, index int, in *Instr, format string, args ...any) {
	e := &VerifyError{Func: v.f.Name, Block: block, Index: index, Msg: fmt.Sprintf(format, args...)}
	if in != nil {
		e.Instr = in.String()
	}
	v.errs = append(v.errs, e)
}

func verifyFunc(p *Program, f *Func, funcs map[*Func]bool, globals map[*Global]bool) []error {
	v := &verifier{p: p, f: f, funcs: funcs, globals: globals}
	if f.File == "" || !validPos(f.Pos) {
		v.errorf(-1, -1, nil, "missing source position")
	}
	for i, t := range f.Params {
		if !validType(t) {
			v.errorf(-1, -1, nil, "parameter %d has invalid type %s", i, t)
		}
	}
	for i, t := range f.Results {
		if !validType(t) {
			v.errorf(-1, -1, nil, "result %d has invalid type %s", i, t)
		}
	}
	if f.Name == "main" && len(f.Params)+len(f.Results) > 0 {
		v.errorf(-1, -1, nil, "main has parameters or results")
	}
	switch {
	case f == p.Init && f.Name != "init":
		v.errorf(-1, -1, nil, "Init is not named init")
	case f == p.Init && len(f.Params)+len(f.Results) > 0:
		v.errorf(-1, -1, nil, "init has parameters or results")
	case f != p.Init && f.Name == "init":
		v.errorf(-1, -1, nil, "a function named init that is not the program's Init")
	}
	for i, s := range f.Slots {
		switch {
		case s == nil:
			v.errorf(-1, -1, nil, "slot %d is nil", i)
		case s.ID != i:
			v.errorf(-1, -1, nil, "slot %d has ID %d", i, s.ID)
		case !validType(s.Type):
			v.errorf(-1, -1, nil, "slot s%d has invalid type %s", i, s.Type)
		case !validPos(s.Pos):
			v.errorf(-1, -1, nil, "slot s%d has no source position", i)
		}
	}
	if f.SSA && len(f.Slots) > 0 {
		v.errorf(-1, -1, nil, "function in SSA form has %d slots", len(f.Slots))
	}
	if len(f.Blocks) == 0 {
		v.errorf(-1, -1, nil, "no blocks")
		return v.errs
	}
	malformed := false
	for i, b := range f.Blocks {
		switch {
		case b == nil:
			v.errorf(i, -1, nil, "nil block")
		case b.ID != i:
			v.errorf(i, -1, nil, "block has ID %d", b.ID)
		default:
			continue
		}
		malformed = true
	}
	if malformed {
		return v.errs // the checks below index blocks by ID
	}
	reachable := v.reachable()
	if f.SSA {
		v.ssa(reachable)
	} else {
		v.unstored = v.unstoredLoads(reachable)
	}
	for i, b := range f.Blocks {
		if !reachable[i] {
			v.errorf(i, -1, nil, "unreachable block: no path from the entry reaches it")
		}
		v.block(b, i)
	}
	return v.errs
}

// successors returns the blocks b's terminator may continue at, ignoring
// targets that are not blocks of the function (reported by block).
func (v *verifier) successors(b *Block) []int {
	if len(b.Instrs) == 0 {
		return nil
	}
	var succ []int
	if t := b.Instrs[len(b.Instrs)-1]; t != nil && (t.Op == Jump || t.Op == Branch) {
		for _, target := range t.Targets {
			if v.isBlock(target) {
				succ = append(succ, target.ID)
			}
		}
	}
	return succ
}

func (v *verifier) isBlock(b *Block) bool {
	return b != nil && b.ID >= 0 && b.ID < len(v.f.Blocks) && v.f.Blocks[b.ID] == b
}

// reachable returns which blocks a path from the entry reaches (D-023
// invariant 1).
func (v *verifier) reachable() []bool {
	seen := make([]bool, len(v.f.Blocks))
	work := []int{0}
	seen[0] = true
	for len(work) > 0 {
		b := work[len(work)-1]
		work = work[:len(work)-1]
		for _, s := range v.successors(v.f.Blocks[b]) {
			if !seen[s] {
				seen[s] = true
				work = append(work, s)
			}
		}
	}
	return seen
}

// unstoredLoads returns the loads of reachable blocks whose slot is not
// stored on every path from the entry to them (D-023 invariant 4). A slot is
// stored on entry to a block b exactly when no path from the entry reaches b
// through blocks that do not store it, the entry included, since nothing is
// stored on entry to it; a load is then checked against the stores before it
// in its block. Loads in unreachable blocks are not checked.
//
// Only a load before any store to its slot in its block needs the CFG. A
// store in a block that strictly dominates the load's settles it, which a
// walk down the dominator tree with a count of stores per slot finds for
// every such load at once: lowered code declares each variable in a block
// that dominates its uses. The entry's own stores, which settle the loads of
// every other block, are applied first, so that a function whose variables
// are all stored in the entry needs no dominators. The loads left, and all of them if the CFG is too
// malformed for dominators, are settled for each slot by a forward search
// from the entry that stops at the blocks storing the slot. The work is the
// instructions, the blocks, and one search per slot left, of the blocks that
// slot can reach unstored.
func (v *verifier) unstoredLoads(reachable []bool) map[*Instr]bool {
	n, ns := len(v.f.Blocks), len(v.f.Slots)
	type query struct {
		in *Instr
		b  int
	}
	queries := make([][]query, n) // by block
	storesIn := make([][]int, ns) // the blocks storing each slot
	blockStores := make([][]int, n)
	mark := make([]int, ns) // block ID + 1 once the slot is stored in that block
	need := false
	for bi, b := range v.f.Blocks {
		for _, in := range b.Instrs {
			if in == nil || in.Op != Load && in.Op != Store || !v.isSlot(in.Slot) {
				continue
			}
			s := in.Slot.ID
			switch {
			case in.Op == Store && mark[s] != bi+1:
				mark[s] = bi + 1
				storesIn[s] = append(storesIn[s], bi)
				blockStores[bi] = append(blockStores[bi], s)
			case in.Op == Load && mark[s] != bi+1 && reachable[bi]:
				queries[bi] = append(queries[bi], query{in, bi})
				need = true
			}
		}
	}
	if !need {
		return nil
	}
	// The entry strictly dominates every other block, so its stores settle
	// the loads of other blocks without dominators.
	inEntry := make([]bool, ns)
	for _, s := range blockStores[0] {
		inEntry[s] = true
	}
	settled := true
	for bi := 1; bi < n; bi++ {
		kept := queries[bi][:0]
		for _, q := range queries[bi] {
			if !inEntry[q.in.Slot.ID] {
				kept = append(kept, q)
			}
		}
		queries[bi] = kept
		settled = settled && len(kept) == 0
	}
	var pending []query
	if settled {
		pending = queries[0]
	} else if d := v.domTree(); d != nil {
		count := make([]int, ns) // stores in the strict dominators of the block visited
		type frame struct{ b, next int }
		stack := []frame{{b: 0}}
		pending = append(pending, queries[0]...)
		for _, s := range blockStores[0] {
			count[s]++
		}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next == len(d.kids[top.b]) {
				for _, s := range blockStores[top.b] {
					count[s]--
				}
				stack = stack[:len(stack)-1]
				continue
			}
			k := d.kids[top.b][top.next]
			top.next++
			for _, q := range queries[k] {
				if count[q.in.Slot.ID] == 0 {
					pending = append(pending, q)
				}
			}
			for _, s := range blockStores[k] {
				count[s]++
			}
			stack = append(stack, frame{b: k})
		}
	} else {
		for _, qs := range queries {
			pending = append(pending, qs...)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	bySlot := make([][]query, ns)
	for _, q := range pending {
		bySlot[q.in.Slot.ID] = append(bySlot[q.in.Slot.ID], q)
	}
	unstored := map[*Instr]bool{}
	stores, open := make([]int, n), make([]int, n) // stamped with the slot + 1
	var work []int
	for s, qs := range bySlot {
		if len(qs) == 0 {
			continue
		}
		stamp := s + 1
		for _, b := range storesIn[s] {
			stores[b] = stamp
		}
		// open: blocks some path from the entry reaches with s unstored.
		open[0] = stamp
		work = append(work[:0], 0)
		for len(work) > 0 {
			b := work[len(work)-1]
			work = work[:len(work)-1]
			if stores[b] == stamp {
				continue
			}
			for _, t := range v.successors(v.f.Blocks[b]) {
				if open[t] != stamp {
					open[t] = stamp
					work = append(work, t)
				}
			}
		}
		for _, q := range qs {
			if open[q.b] == stamp {
				unstored[q.in] = true
			}
		}
	}
	return unstored
}

// domTree returns the function's dominator tree, or nil if its CFG is too
// malformed to analyze: Dominators follows the last instruction's targets,
// so they must be blocks of the function, and only jump and branch may have
// any (otherwise reported by signature).
func (v *verifier) domTree() *DomTree {
	for _, b := range v.f.Blocks {
		if len(b.Instrs) == 0 {
			continue
		}
		t := b.Instrs[len(b.Instrs)-1]
		if t == nil || t.Op != Jump && t.Op != Branch && len(t.Targets) > 0 {
			return nil
		}
		for _, target := range t.Targets {
			if !v.isBlock(target) {
				return nil
			}
		}
	}
	return Dominators(v.f)
}

// block checks one block; unstoredLoads has settled its loads. Operands must be defined earlier in the same block (D-023), so
// "defined earlier in the block" is an exact dominance check.
func (v *verifier) block(b *Block, bi int) {
	if len(b.Instrs) == 0 {
		v.errorf(bi, -1, nil, "empty block: missing terminator")
		return
	}
	defined := map[*Instr]bool{}
	ids := v.ids // value numbers are unique in the whole of an SSA function
	if ids == nil {
		ids = map[int]bool{}
	}
	body := false // whether a non-phi instruction came earlier
	for i, in := range b.Instrs {
		if in == nil {
			v.errorf(bi, i, nil, "nil instruction")
			continue
		}
		last := i == len(b.Instrs)-1
		switch {
		case in.Op.IsTerminator() && !last:
			v.errorf(bi, i, in, "terminator before the end of the block")
		case !in.Op.IsTerminator() && last:
			v.errorf(bi, i, in, "missing terminator: block does not end in ret, jump, branch, or panic")
		}
		if !validPos(in.Pos) {
			v.errorf(bi, i, in, "missing source position")
		}
		for _, a := range in.Args {
			switch {
			case a == nil:
				v.errorf(bi, i, in, "nil operand")
			case defined[a]:
			case v.f.SSA && in.Op == Phi:
				// Checked against its incoming edge by phi.
			case v.f.SSA:
				v.dominatingDef(bi, i, in, a)
			default:
				v.errorf(bi, i, in, "operand v%d is not defined earlier in this block", a.ID)
			}
		}
		v.signature(bi, i, in)
		switch {
		case v.f.SSA && (in.Op == Load || in.Op == Store):
			v.errorf(bi, i, in, "%s in a function in SSA form", in.Op)
		case in.Op == Load || in.Op == Store:
			v.slotRef(bi, i, in)
		case v.f.SSA && in.Op == Phi:
			if bi == 0 {
				v.errorf(bi, i, in, "phi in the entry block")
			}
			if body {
				v.errorf(bi, i, in, "phi after a non-phi instruction")
			}
			v.phi(bi, i, in)
		}
		body = body || in.Op != Phi
		if in.Type != NoType {
			if ids[in.ID] {
				v.errorf(bi, i, in, "value number v%d defined twice", in.ID)
			}
			ids[in.ID] = true
			defined[in] = true
		}
	}
}

// signature checks an instruction's result type, value number, operand
// count and types, and targets against its operation.
func (v *verifier) signature(bi, i int, in *Instr) {
	var want Type
	var args []Type // the operand types; nil with anyArgs for println
	// integer is the type of the first operand if it is an integer type,
	// for operations whose operands share any integer type; I64 otherwise,
	// so that a wrong operand is reported against i64.
	integer := I64
	if len(in.Args) > 0 && in.Args[0] != nil && in.Args[0].Type.IsInteger() {
		integer = in.Args[0].Type
	}
	// float is the same for the float operations, with F64 by default.
	float := F64
	if len(in.Args) > 0 && in.Args[0] != nil && in.Args[0].Type.IsFloat() {
		float = in.Args[0].Type
	}
	anyArgs, targets := false, 0
	switch in.Op {
	case Const:
		want = in.Type
		switch {
		case !validType(in.Type):
			v.errorf(bi, i, in, "const has invalid type %s", in.Type)
		case in.Type == Bool && in.Const != 0 && in.Const != 1:
			v.errorf(bi, i, in, "bool constant %d is not 0 or 1", in.Const)
		case in.Type.Canonical(in.Const) != in.Const:
			v.errorf(bi, i, in, "%s constant %d is not in canonical form", in.Type, in.Const)
		}
	case Load:
		want = I64
		if v.isSlot(in.Slot) {
			want = in.Slot.Type
		}
	case Store:
		args = []Type{I64}
		if v.isSlot(in.Slot) {
			args[0] = in.Slot.Type
		}
	case Shl, Shr:
		want, args = integer, []Type{integer, U64}
		if len(in.Args) == 2 && in.Args[1] != nil && in.Args[1].Type.IsInteger() && !in.Args[1].Type.Signed() {
			args[1] = in.Args[1].Type // any unsigned count
		}
	case Panic:
		if in.Index < 0 || in.Index >= len(PanicMessages) {
			v.errorf(bi, i, in, "panic of unknown kind %d", in.Index)
		}
	case Add, Sub, Mul, And, Or, Xor, AndNot, Div, Rem:
		want, args = integer, []Type{integer, integer}
	case Neg, Com:
		want, args = integer, []Type{integer}
	case Conv:
		want, args = in.Type, []Type{integer}
		if !in.Type.IsInteger() {
			v.errorf(bi, i, in, "conv to %s, not an integer type", in.Type)
		}
	case Eq, Ne:
		t := integer
		if len(in.Args) > 0 && in.Args[0] != nil && in.Args[0].Type == Bool {
			t = Bool
		}
		want, args = Bool, []Type{t, t}
	case Lt, Le, Gt, Ge:
		want, args = Bool, []Type{integer, integer}
	case Not:
		want, args = Bool, []Type{Bool}
	case FAdd, FSub, FMul, FDiv:
		want, args = float, []Type{float, float}
	case FNeg:
		want, args = float, []Type{float}
	case FEq, FNe, FLt, FLe, FGt, FGe:
		want, args = Bool, []Type{float, float}
	case IToF, FToF, FToI:
		want, args = in.Type, []Type{float}
		if in.Op == IToF {
			args[0] = integer
		}
		if in.Op == FToI && !in.Type.IsInteger() {
			v.errorf(bi, i, in, "ftoi to %s, not an integer type", in.Type)
		} else if in.Op != FToI && !in.Type.IsFloat() {
			v.errorf(bi, i, in, "%s to %s, not a float type", in.Op, in.Type)
		}
	case Println, Print:
		anyArgs = true
	case Phi:
		want = in.Type
		if !v.f.SSA {
			v.errorf(bi, i, in, "phi in a function not in SSA form")
		}
		if !validType(in.Type) {
			v.errorf(bi, i, in, "phi has invalid type %s", in.Type)
		}
		args = make([]Type, len(in.Edges)) // one value per edge
		for j := range args {
			args[j] = in.Type
		}
	case Ret:
		args = v.f.Results
	case Jump:
		targets = 1
	case Branch:
		args, targets = []Type{Bool}, 2
	case Param:
		switch {
		case bi != 0:
			v.errorf(bi, i, in, "param outside the entry block")
		case in.Index < 0 || in.Index >= len(v.f.Params):
			v.errorf(bi, i, in, "param %d of a function with %d parameters", in.Index, len(v.f.Params))
		default:
			want = v.f.Params[in.Index]
		}
	case Call:
		args = make([]Type, len(in.Args))
		for j, a := range in.Args {
			if a != nil {
				args[j] = a.Type
			}
		}
		if in.Callee == nil || !v.funcs[in.Callee] {
			v.errorf(bi, i, in, "callee is not a function of the program")
			break
		}
		if in.Callee == v.p.Init {
			v.errorf(bi, i, in, "call of init")
		}
		want, args = callType(in.Callee), in.Callee.Params
	case GLoad, GStore:
		if !v.globals[in.Global] {
			// Report only this, not the types it leaves unknown.
			v.errorf(bi, i, in, "global operand is not a global of the program")
			want = in.Type
			for _, a := range in.Args {
				if a != nil {
					args = append(args, a.Type)
				}
			}
			break
		}
		if in.Op == GLoad {
			want = in.Global.Type
		} else {
			args = []Type{in.Global.Type}
		}
	case Extract:
		// The tuple must be a call in this block (checked with the
		// operands), so its callee is known.
		args = []Type{Tuple}
		if len(in.Args) == 1 && in.Args[0] != nil && in.Args[0].Op == Call && in.Args[0].Callee != nil {
			if rs := in.Args[0].Callee.Results; in.Index >= 0 && in.Index < len(rs) {
				want = rs[in.Index]
			} else {
				v.errorf(bi, i, in, "extract %d of a call with %d results", in.Index, len(rs))
			}
		} else {
			v.errorf(bi, i, in, "extract operand is not a call")
		}
	default:
		v.errorf(bi, i, in, "invalid operation %s", in.Op)
		return
	}
	if in.Type != want {
		v.errorf(bi, i, in, "%s has result type %s, want %s", in.Op, in.Type, want)
	}
	if (in.Type == NoType) != (in.ID == -1) {
		v.errorf(bi, i, in, "value number %d inconsistent with result type %s", in.ID, in.Type)
	}
	if !anyArgs && len(in.Args) != len(args) {
		v.errorf(bi, i, in, "%s has %d operands, want %d", in.Op, len(in.Args), len(args))
	}
	for j, a := range in.Args {
		switch {
		case a == nil:
		case a.Type == Tuple && in.Op != Extract:
			v.errorf(bi, i, in, "tuple v%d used by %s; only extract may use a tuple", a.ID, in.Op)
		case anyArgs && !validType(a.Type):
			v.errorf(bi, i, in, "operand v%d has invalid type %s", a.ID, a.Type)
		case !anyArgs && j < len(args) && a.Type != args[j]:
			v.errorf(bi, i, in, "operand v%d has type %s, want %s", a.ID, a.Type, args[j])
		}
	}
	if len(in.Targets) != targets {
		v.errorf(bi, i, in, "%s has %d targets, want %d", in.Op, len(in.Targets), targets)
	}
	for _, t := range in.Targets {
		if !v.isBlock(t) {
			v.errorf(bi, i, in, "target is not a block of this function")
		}
	}
	if in.Op != Const && in.Const != 0 {
		v.errorf(bi, i, in, "%s has a constant operand", in.Op)
	}
	if in.Op != Load && in.Op != Store && in.Slot != nil {
		v.errorf(bi, i, in, "%s has a slot operand", in.Op)
	}
	if in.Op != Param && in.Op != Extract && in.Op != Panic && in.Index != 0 {
		v.errorf(bi, i, in, "%s has a parameter index", in.Op)
	}
	if in.Op != Call && in.Callee != nil {
		v.errorf(bi, i, in, "%s has a callee", in.Op)
	}
	if in.Op != GLoad && in.Op != GStore && in.Global != nil {
		v.errorf(bi, i, in, "%s has a global operand", in.Op)
	}
	if in.Op != Phi && in.Edges != nil {
		v.errorf(bi, i, in, "%s has incoming edges", in.Op)
	}
}

func (v *verifier) isSlot(s *Slot) bool {
	return s != nil && s.ID >= 0 && s.ID < len(v.f.Slots) && v.f.Slots[s.ID] == s
}

// slotRef checks a Load or Store's slot: it must belong to the function,
// and a load must follow a store to the same slot on every path.
func (v *verifier) slotRef(bi, i int, in *Instr) {
	s := in.Slot
	if !v.isSlot(s) {
		v.errorf(bi, i, in, "slot operand is not a slot of this function")
		return
	}
	if in.Op == Load && v.unstored[in] {
		v.errorf(bi, i, in, "load of s%d (%s) before any store to it on every path", s.ID, s.Name)
	}
}

func validType(t Type) bool { return t == Bool || t.IsInteger() || t.IsFloat() }

func validPos(p Pos) bool { return p.Line > 0 && p.Col > 0 }
