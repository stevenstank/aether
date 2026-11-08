package amd64

// Instruction selection for aether build: EmitOptions.Select.
//
// The accumulator sequences of instr compute everything through %rax. Select
// keeps them where it has nothing better, and adds two things:
//
// Immediates. A constant that fits a sign-extended 32-bit immediate, or a Conv
// of one whose result also fits, has no location: no code, register, or cell.
// Every read of it is the operand $c, and x86-64 sign-extends the immediate
// of every 64-bit instruction, so each sequence that reads an operand reads
// the constant's canonical 64 bits. Removing these values also lowers register
// pressure, and values are pure, so nothing observable moves (D-024).
//
// Fused branches. A comparison or Not whose only use is its block's Branch,
// with no code between it and the Branch, produces no value: the Branch
// compares and jumps on the condition itself. Only immediates may lie between,
// and those write no register, so the operands the allocator placed at the
// comparison are still there at the Branch.
//
// checkSelection verifies both independently of selectFunc. The allocator
// gives neither kind of value a register, and the frame no cell.
//
// Every sequence here writes flags only just before the instruction that reads
// them. Loading a constant may clobber the flags (xorl), and loads always come
// before the comparison that sets them.

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/stevenstank/aether/internal/ir"
)

// condition is what a Branch tests: v, negated if neg. If v is in skip it is
// a fused comparison, and otherwise a value read at the Branch.
type condition struct {
	v   *ir.Instr
	neg bool
}

// selection is the result of selectFunc.
type selection struct {
	imm  map[*ir.Instr]int64     // values that are immediates
	skip valueSet                // comparisons and Nots fused into a Branch
	cond map[*ir.Instr]condition // by Branch
	fwd  map[*ir.Block]*ir.Block // emitter: blocks that only jump, to their final target
	// twoAddr enables the register forms of arith.go.
	twoAddr bool
}

func fitsImm(c int64) bool { return c >= math.MinInt32 && c <= math.MaxInt32 }

// useCounts returns the number of uses of each value of f.
func useCounts(f *ir.Func) map[*ir.Instr]int {
	uses := map[*ir.Instr]int{}
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			for _, a := range in.Args {
				uses[a]++
			}
		}
	}
	return uses
}

// previousCode returns the index of the last instruction of b before i that
// is not an immediate, or -1.
func (s *selection) previousCode(b *ir.Block, i int) int {
	for i--; i >= 0; i-- {
		if _, ok := s.imm[b.Instrs[i]]; !ok {
			return i
		}
	}
	return -1
}

// selectFunc chooses the immediates and fused branches of f.
func selectFunc(f *ir.Func) *selection {
	s := &selection{imm: map[*ir.Instr]int64{}, skip: valueSet{}, cond: map[*ir.Instr]condition{}}
	uses := useCounts(f)
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			switch in.Op {
			case ir.Const:
				// A float operand goes to a vector register, which takes no
				// immediate: each use of an immediate float would cost a move
				// through %rax, so a float constant is materialized once.
				if fitsImm(in.Const) && !in.Type.IsFloat() {
					s.imm[in] = in.Const
				}
			case ir.Conv:
				if c, ok := s.imm[in.Args[0]]; ok && fitsImm(in.Type.Canonical(c)) {
					s.imm[in] = in.Type.Canonical(c)
				}
			}
		}
	}
	for _, b := range f.Blocks {
		i := len(b.Instrs) - 1
		if i < 0 || b.Instrs[i].Op != ir.Branch {
			continue
		}
		br := b.Instrs[i]
		c := condition{v: br.Args[0]}
		for uses[c.v] == 1 && (c.v.Op == ir.Not || c.v.Op.IsComparison()) {
			j := s.previousCode(b, i)
			if j < 0 || b.Instrs[j] != c.v {
				break
			}
			s.skip[c.v] = true
			i = j
			if c.v.Op != ir.Not {
				break
			}
			c = condition{c.v.Args[0], !c.neg}
		}
		s.cond[br] = c
	}
	return s
}

// noLocation returns the values that need neither a register nor a cell.
func (s *selection) noLocation() valueSet {
	none := valueSet{}
	for v := range s.imm {
		none[v] = true
	}
	for v := range s.skip {
		none[v] = true
	}
	return none
}

// checkSelection verifies s, a selection for f with allocation regs,
// independently of how selectFunc found it.
func checkSelection(f *ir.Func, s *selection, regs map[*ir.Instr]int) error {
	errf := func(format string, args ...any) error {
		return fmt.Errorf("amd64: func %s: selection: %s", f.Name, fmt.Sprintf(format, args...))
	}
	for _, v := range sortedValues(s.noLocation()) {
		if _, ok := regs[v]; ok {
			return errf("v%d has no location, but a register", v.ID)
		}
	}
	for _, v := range sortedValues(keys64(s.imm)) {
		c := s.imm[v]
		switch {
		case !fitsImm(c):
			return errf("v%d: %d does not fit an immediate", v.ID, c)
		case v.Op == ir.Const && c == v.Const:
		case v.Op == ir.Conv:
			if a, ok := s.imm[v.Args[0]]; !ok || v.Type.Canonical(a) != c {
				return errf("v%d is not the immediate %d", v.ID, c)
			}
		default:
			return errf("v%d is not the immediate %d", v.ID, c)
		}
	}
	uses := useCounts(f)
	fused := 0
	for _, b := range f.Blocks {
		i := len(b.Instrs) - 1
		if i < 0 || b.Instrs[i].Op != ir.Branch {
			continue
		}
		br := b.Instrs[i]
		v, neg := br.Args[0], false
		for s.skip[v] {
			j := s.previousCode(b, i)
			switch {
			case j < 0 || b.Instrs[j] != v:
				return errf("v%d is fused into a branch of b%d, with code between", v.ID, b.ID)
			case uses[v] != 1:
				return errf("v%d is fused into a branch, but has %d uses", v.ID, uses[v])
			case v.Op != ir.Not && !v.Op.IsComparison():
				return errf("%s cannot be fused", v)
			}
			fused++
			i = j
			if v.Op != ir.Not {
				break
			}
			v, neg = v.Args[0], !neg
		}
		if c := s.cond[br]; c.v != v || c.neg != neg {
			return errf("the branch of b%d tests the wrong condition", b.ID)
		}
	}
	if fused != len(s.skip) {
		return errf("%d values are fused, but %d are fused into branches", len(s.skip), fused)
	}
	return nil
}

func keys64(m map[*ir.Instr]int64) valueSet {
	s := valueSet{}
	for v := range m {
		s[v] = true
	}
	return s
}

// subRegs gives the 32-, 16-, and 8-bit names of each 64-bit register.
var subRegs = map[string][3]string{
	"%rax": {"%eax", "%ax", "%al"}, "%rcx": {"%ecx", "%cx", "%cl"}, "%rdx": {"%edx", "%dx", "%dl"},
	"%rbx": {"%ebx", "%bx", "%bl"}, "%rsi": {"%esi", "%si", "%sil"}, "%rdi": {"%edi", "%di", "%dil"},
	"%r8": {"%r8d", "%r8w", "%r8b"}, "%r9": {"%r9d", "%r9w", "%r9b"}, "%r10": {"%r10d", "%r10w", "%r10b"},
	"%r11": {"%r11d", "%r11w", "%r11b"}, "%r12": {"%r12d", "%r12w", "%r12b"}, "%r13": {"%r13d", "%r13w", "%r13b"},
	"%r14": {"%r14d", "%r14w", "%r14b"}, "%r15": {"%r15d", "%r15w", "%r15b"},
}

func isReg(s string) bool { return s[0] == '%' }
func isImm(s string) bool { return s[0] == '$' }

// materialize loads c into the register reg by its shortest form. A 32-bit
// write zeroes the upper half. It may clobber the flags.
func (e *emitter) materialize(c int64, reg string) {
	switch {
	case c == 0:
		e.op("xorl\t%s, %s", subRegs[reg][0], subRegs[reg][0])
	case c > 0 && c <= math.MaxUint32:
		e.op("movl\t$%d, %s", c, subRegs[reg][0])
	case fitsImm(c):
		e.op("movq\t$%d, %s", c, reg)
	default:
		e.op("movabsq\t$%d, %s", c, reg)
	}
}

// load reads v into the register reg.
func (e *emitter) load(v *ir.Instr, reg string) {
	e.move(e.val(v), reg)
}

// move copies a location or immediate to a register, or an immediate or a
// register to memory. It may clobber the flags if the source is an immediate.
func (e *emitter) move(src, dst string) {
	switch {
	case src == dst:
	case isImm(src) && e.sel != nil && isReg(dst):
		c, err := strconv.ParseInt(src[1:], 10, 64)
		if err != nil {
			e.fail(fmt.Errorf("bad immediate %s", src))
			return
		}
		e.materialize(c, dst)
	default:
		e.op("movq\t%s, %s", src, dst)
	}
}

// put writes v to the memory operand mem, directly if v is an immediate.
func (e *emitter) put(v *ir.Instr, mem string) {
	src := e.val(v)
	if isImm(src) {
		e.op("movq\t%s, %s", src, mem)
		return
	}
	e.op("movq\t%s, %%rax", src)
	e.op("movq\t%%rax, %s", mem)
}

func (e *emitter) fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

// target is the register a sequence for in computes in: its own, or %rax if
// it lives in a cell. finish completes the sequence.
func (e *emitter) target(in *ir.Instr) string {
	if dst := e.val(in); isReg(dst) {
		return dst
	}
	return "%rax"
}

func (e *emitter) finish(in *ir.Instr, t string) {
	if dst := e.val(in); dst != t {
		e.op("movq\t%s, %s", t, dst)
	}
}

// canonReg puts the register reg in the canonical form of the integer type t,
// if t is narrower than 64 bits.
func (e *emitter) canonReg(reg string, t ir.Type) {
	if t.IsInteger() && t.Width() < 64 {
		e.canonFrom(reg, reg, t)
	}
}

// selected emits in if selection has a form for it, and reports whether it
// did. Otherwise instr emits its usual sequence, which reads immediates
// through val.
func (e *emitter) selected(blk *ir.Block, in *ir.Instr) bool {
	s := e.sel
	if _, ok := s.imm[in]; ok || s.skip[in] {
		return true
	}
	switch in.Op {
	case ir.Const:
		// Too large for an immediate, or a float constant (selectFunc).
		if dst := e.val(in); fitsImm(in.Const) && !isReg(dst) {
			e.op("movq\t$%d, %s", in.Const, dst)
			break
		}
		t := e.target(in)
		e.materialize(in.Const, t)
		e.finish(in, t)
	case ir.Add, ir.Sub, ir.Mul, ir.And, ir.Or, ir.Xor, ir.AndNot:
		return e.selectBinary(in) || e.twoAddress(in)
	case ir.Neg, ir.Com, ir.Conv:
		return e.twoAddress(in)
	case ir.Div, ir.Rem:
		return e.selectDivide(in)
	case ir.Shl, ir.Shr:
		return e.selectShift(in)
	case ir.Eq, ir.Ne, ir.Lt, ir.Le, ir.Gt, ir.Ge:
		cc := e.compare(in)
		e.op("set%s\t%%al", cc)
		t := e.target(in)
		e.op("movzbl\t%%al, %s", subRegs[t][0])
		e.finish(in, t)
	case ir.Not:
		t := e.target(in)
		e.load(in.Args[0], t)
		e.op("xorq\t$1, %s", t)
		e.finish(in, t)
	case ir.Jump:
		e.edgeCopies(ir.Edge{From: blk, Index: 0})
		e.jumpTo(in.Targets[0])
	case ir.Branch:
		e.selectBranch(blk, in)
	default:
		return false
	}
	return true
}

// selectBinary emits an operation with an immediate operand. An integer
// operation's low width bits are exact, so one that can carry into the upper
// bits is canonicalized. The bitwise operations of canonical values are
// already canonical: each bit above the width is the same function of bits
// that are all equal to the sign bit or all zero.
func (e *emitter) selectBinary(in *ir.Instr) bool {
	x, y := in.Args[0], in.Args[1]
	c, ok := e.sel.imm[y]
	if !ok {
		if in.Op == ir.Sub || in.Op == ir.AndNot {
			return false
		}
		if c, ok = e.sel.imm[x]; !ok {
			return false
		}
		x = y
	}
	t, xs := e.target(in), e.val(x)
	switch in.Op {
	case ir.Mul:
		if isImm(xs) {
			e.load(x, t)
			xs = t
		}
		e.op("imulq\t$%d, %s, %s", c, xs, t)
	case ir.Add, ir.Sub:
		d, mnemonic := c, "addq"
		if in.Op == ir.Sub {
			d, mnemonic = -c, "subq"
		}
		if isReg(xs) && xs != t && fitsImm(d) {
			e.op("leaq\t%d(%s), %s", d, xs, t)
		} else {
			e.load(x, t)
			e.op("%s\t$%d, %s", mnemonic, c, t)
		}
	case ir.AndNot:
		e.load(x, t)
		e.op("andq\t$%d, %s", ^c, t)
	default:
		e.load(x, t)
		e.op("%s\t$%d, %s", map[ir.Op]string{ir.And: "andq", ir.Or: "orq", ir.Xor: "xorq"}[in.Op], c, t)
	}
	if in.Op == ir.Add || in.Op == ir.Sub || in.Op == ir.Mul {
		e.canonReg(t, in.Type)
	}
	e.finish(in, t)
	return true
}

// selectDivide emits a division by an immediate, which needs no test for -1:
// the divisor is known. Lowering checked that it is not zero.
func (e *emitter) selectDivide(in *ir.Instr) bool {
	c, ok := e.sel.imm[in.Args[1]]
	if !ok {
		return false
	}
	switch {
	case in.Type.Signed() && c == -1 && in.Op == ir.Div:
		e.load(in.Args[0], "%rax")
		e.op("negq\t%%rax")
	case in.Type.Signed() && c == -1:
		e.materialize(0, "%rax")
	default:
		e.move(fmt.Sprintf("$%d", c), "%rcx")
		e.load(in.Args[0], "%rax")
		if in.Type.Signed() {
			e.op("cqto")
			e.op("idivq\t%%rcx")
		} else {
			e.op("xorl\t%%edx, %%edx")
			e.op("divq\t%%rcx")
		}
		if in.Op == ir.Rem {
			e.op("movq\t%%rdx, %%rax")
		}
	}
	e.canonReg("%rax", in.Type)
	e.finish(in, "%rax")
	return true
}

// selectShift emits a shift by an immediate count, which is unsigned. A count
// at or above the width gives 0 or the sign fill. A right shift of a canonical
// value in range stays canonical.
func (e *emitter) selectShift(in *ir.Instr) bool {
	c, ok := e.sel.imm[in.Args[1]]
	if !ok {
		return false
	}
	x, n := in.Args[0], uint64(c)
	t := e.target(in)
	inRange := n < uint64(x.Type.Width())
	switch {
	case !inRange && (in.Op == ir.Shl || !x.Type.Signed()):
		e.materialize(0, t)
	case !inRange:
		e.load(x, t)
		e.op("sarq\t$63, %s", t)
	case in.Op == ir.Shl:
		e.load(x, t)
		e.op("shlq\t$%d, %s", n, t)
	case x.Type.Signed():
		e.load(x, t)
		e.op("sarq\t$%d, %s", n, t)
	default:
		e.load(x, t)
		e.op("shrq\t$%d, %s", n, t)
	}
	if in.Op == ir.Shl || in.Type != x.Type {
		e.canonReg(t, in.Type)
	}
	e.finish(in, t)
	return true
}

// swapped is the comparison that holds of y, x when op holds of x, y.
var swapped = map[ir.Op]ir.Op{ir.Eq: ir.Eq, ir.Ne: ir.Ne, ir.Lt: ir.Gt, ir.Le: ir.Ge, ir.Gt: ir.Lt, ir.Ge: ir.Le}

// inverse is the condition code that holds exactly when cc does not.
var inverse = map[string]string{"e": "ne", "ne": "e", "l": "ge", "ge": "l", "le": "g", "g": "le", "b": "ae", "ae": "b", "be": "a", "a": "be"}

// compare emits the comparison c and returns the condition code that holds
// when c is true. x86 compares only with the immediate as the second operand,
// so an immediate on the left swaps the operands and the condition. After
// testq r, r the flags are those of cmpq $0, r: OF and CF are clear, and ZF
// and SF are the same, so every condition code means the same.
func (e *emitter) compare(c *ir.Instr) string {
	x, y, op := c.Args[0], c.Args[1], c.Op
	xs, ys := e.val(x), e.val(y)
	if isImm(xs) && !isImm(ys) {
		xs, ys, op = ys, xs, swapped[op]
	}
	cc := strings.TrimPrefix(setcc[op], "set")
	if t := x.Type; t.IsInteger() && !t.Signed() {
		cc = strings.TrimPrefix(setccUnsigned[op], "set")
	}
	if isImm(xs) || !isReg(xs) && !isReg(ys) && !isImm(ys) {
		e.move(xs, "%rax")
		xs = "%rax"
	}
	if ys == "$0" && isReg(xs) {
		e.op("testq\t%s, %s", xs, xs)
	} else {
		e.op("cmpq\t%s, %s", ys, xs)
	}
	return cc
}

// selectBranch emits a Branch: the test, then the conditional jump and the
// edges' copies laid out so that the common cases need one jump. Copies come
// after the conditional jump, on the path of their edge only.
func (e *emitter) selectBranch(blk *ir.Block, in *ir.Instr) {
	then, els := ir.Edge{From: blk, Index: 0}, ir.Edge{From: blk, Index: 1}
	tt, et := e.resolve(in.Targets[0]), e.resolve(in.Targets[1])
	tc, ec := e.hasCopies(then), e.hasCopies(els)
	if tt == et && !tc && !ec {
		e.jumpTo(tt)
		return
	}
	cc := e.condition(in)
	jcc := func(cc string, b *ir.Block) { e.op("j%s\t%s", cc, e.label(b)) }
	switch {
	case !tc && !ec && et == e.next:
		jcc(cc, tt)
	case !tc && !ec && tt == e.next:
		jcc(inverse[cc], et)
	case !tc:
		jcc(cc, tt)
		e.edgeCopies(els)
		e.jumpTo(et)
	case !ec:
		jcc(inverse[cc], et)
		e.edgeCopies(then)
		e.jumpTo(tt)
	default:
		local := e.label(blk) + ".1"
		first, second, firstT, secondT, c := then, els, tt, et, inverse[cc]
		if tt == e.next {
			first, second, firstT, secondT, c = els, then, et, tt, cc
		}
		e.op("j%s\t%s", c, local)
		e.edgeCopies(first)
		e.op("jmp\t%s", e.label(firstT))
		fmt.Fprintf(e.b, "%s:\n", local)
		e.edgeCopies(second)
		e.jumpTo(secondT)
	}
}

// condition emits the test of a Branch and returns the condition code that
// holds when the branch goes to its first target.
func (e *emitter) condition(br *ir.Instr) string {
	c := e.sel.cond[br]
	var cc string
	if e.sel.skip[c.v] {
		cc = e.compare(c.v)
	} else {
		cc = "ne"
		switch vs := e.val(c.v); {
		case isReg(vs):
			e.op("testq\t%s, %s", vs, vs)
		case isImm(vs):
			e.load(c.v, "%rax")
			e.op("testq\t%%rax, %%rax")
		default:
			e.op("cmpq\t$0, %s", vs)
		}
	}
	if c.neg {
		return inverse[cc]
	}
	return cc
}

// hasCopies reports whether edge's phis need any code.
func (e *emitter) hasCopies(edge ir.Edge) bool {
	blk := edge.From.Instrs[len(edge.From.Instrs)-1].Targets[edge.Index]
	for _, phi := range blk.Instrs {
		if phi.Op != ir.Phi {
			break
		}
		if e.val(phi.Args[indexOfEdge(phi, edge)]) != e.val(phi) {
			return true
		}
	}
	return false
}

// jumpTo continues at b, whose code may simply come next.
func (e *emitter) jumpTo(b *ir.Block) {
	if b = e.resolve(b); b != e.next {
		e.op("jmp\t%s", e.label(b))
	}
}

// resolve returns the block a jump to b may go to instead.
func (e *emitter) resolve(b *ir.Block) *ir.Block {
	if t, ok := e.sel.fwd[b]; ok {
		return t
	}
	return b
}

// forwarders finds the blocks with no code but a jump whose edge has no
// copies: a jump to one may go straight to the final target, and the block
// need not be emitted. The entry block stays, since the prologue runs into
// it. On a cycle of such blocks, which is an empty infinite loop, one stays,
// and the others forward to it.
func (e *emitter) forwarders() map[*ir.Block]*ir.Block {
	next := map[*ir.Block]*ir.Block{}
	for _, b := range e.f.Blocks[1:] {
		only := true
		for _, in := range b.Instrs[:len(b.Instrs)-1] {
			if _, ok := e.sel.imm[in]; !ok && in.Op != ir.Phi {
				only = false
				break
			}
		}
		last := b.Instrs[len(b.Instrs)-1]
		if only && last.Op == ir.Jump && last.Targets[0] != b && !e.hasCopies(ir.Edge{From: b, Index: 0}) {
			next[b] = last.Targets[0]
		}
	}
	final := map[*ir.Block]*ir.Block{}
	for _, b := range e.f.Blocks {
		if _, ok := next[b]; !ok {
			continue
		}
		var path []*ir.Block
		on := map[*ir.Block]bool{}
		t := b
		for {
			n, ok := next[t]
			if !ok {
				break
			}
			if f, done := final[t]; done {
				t = f
				break
			}
			if on[t] {
				delete(next, t)
				break
			}
			on[t] = true
			path = append(path, t)
			t = n
		}
		for _, p := range path {
			if p != t {
				final[p] = t
			}
		}
	}
	return final
}
